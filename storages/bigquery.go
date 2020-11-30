package storages

import (
	"context"
	"errors"
	"fmt"
	"github.com/hashicorp/go-multierror"
	"github.com/jitsucom/eventnative/adapters"
	"github.com/jitsucom/eventnative/appconfig"
	"github.com/jitsucom/eventnative/caching"
	"github.com/jitsucom/eventnative/counters"
	"github.com/jitsucom/eventnative/events"
	"github.com/jitsucom/eventnative/logging"
	"github.com/jitsucom/eventnative/metrics"
	"github.com/jitsucom/eventnative/parsers"
	"github.com/jitsucom/eventnative/safego"
	"github.com/jitsucom/eventnative/schema"
	"github.com/jitsucom/eventnative/typing"
	"time"
)

//Store files to google BigQuery in two modes:
//batch: via google cloud storage in batch mode (1 file = 1 transaction)
//stream: via events queue in stream mode (1 object = 1 transaction)
type BigQuery struct {
	name            string
	gcsAdapter      *adapters.GoogleCloudStorage
	bqAdapter       *adapters.BigQuery
	tableHelper     *TableHelper
	schemaProcessor *schema.Processor
	streamingWorker *StreamingWorker
	fallbackLogger  *events.AsyncLogger
	eventsCache     *caching.EventsCache
	breakOnError    bool

	closed bool
}

func NewBigQuery(ctx context.Context, name string, eventQueue *events.PersistentQueue, config *adapters.GoogleConfig,
	processor *schema.Processor, breakOnError, streamMode bool, monitorKeeper MonitorKeeper, fallbackLoggerFactoryMethod func() *events.AsyncLogger,
	queryLogger *logging.QueryLogger, eventsCache *caching.EventsCache) (*BigQuery, error) {
	var gcsAdapter *adapters.GoogleCloudStorage
	if !streamMode {
		var err error
		gcsAdapter, err = adapters.NewGoogleCloudStorage(ctx, config)
		if err != nil {
			return nil, err
		}
	}

	bigQueryAdapter, err := adapters.NewBigQuery(ctx, config, queryLogger)
	if err != nil {
		return nil, err
	}

	//create dataset if doesn't exist
	err = bigQueryAdapter.CreateDataset(config.Dataset)
	if err != nil {
		bigQueryAdapter.Close()
		if gcsAdapter != nil {
			gcsAdapter.Close()
		}
		return nil, err
	}

	tableHelper := NewTableHelper(bigQueryAdapter, monitorKeeper, BigQueryType)

	bq := &BigQuery{
		name:            name,
		gcsAdapter:      gcsAdapter,
		bqAdapter:       bigQueryAdapter,
		tableHelper:     tableHelper,
		schemaProcessor: processor,
		fallbackLogger:  fallbackLoggerFactoryMethod(),
		eventsCache:     eventsCache,
		breakOnError:    breakOnError,
	}

	if streamMode {
		bq.streamingWorker = newStreamingWorker(eventQueue, processor, bq, eventsCache)
		bq.streamingWorker.start()
	} else {
		bq.startBatch()
	}

	return bq, nil
}

//Periodically (every 30 seconds):
//1. get all files from google cloud storage
//2. load them to BigQuery via google api
//3. delete file from google cloud storage
func (bq *BigQuery) startBatch() {
	safego.RunWithRestart(func() {
		for {
			if bq.closed {
				break
			}
			//TODO configurable
			time.Sleep(30 * time.Second)

			filesKeys, err := bq.gcsAdapter.ListBucket(appconfig.Instance.ServerName)
			if err != nil {
				logging.Errorf("[%s] Error reading files from google cloud storage: %v", bq.Name(), err)
				continue
			}

			if len(filesKeys) == 0 {
				continue
			}

			for _, fileKey := range filesKeys {
				tableName, tokenId, rowsCount, err := extractDataFromFileName(fileKey)
				if err != nil {
					logging.Errorf("[%s] Google cloud storage file [%s] has wrong format: %v", bq.Name(), fileKey, err)
					continue
				}

				if err := bq.bqAdapter.Copy(fileKey, tableName); err != nil {
					logging.Errorf("[%s] Error copying file [%s] from google cloud storage to BigQuery: %v", bq.Name(), fileKey, err)
					metrics.ErrorTokenEvents(tokenId, bq.Name(), rowsCount)
					counters.ErrorEvents(bq.Name(), rowsCount)
					continue
				}

				metrics.SuccessTokenEvents(tokenId, bq.Name(), rowsCount)
				counters.SuccessEvents(bq.Name(), rowsCount)

				if err := bq.gcsAdapter.DeleteObject(fileKey); err != nil {
					logging.SystemErrorf("[%s] file %s wasn't deleted from google cloud storage and will be inserted in db again: %v", bq.Name(), fileKey, err)
					continue
				}
			}
		}
	})
}

//Insert fact in BigQuery
func (bq *BigQuery) Insert(dataSchema *schema.Table, fact events.Fact) (err error) {
	dbSchema, err := bq.tableHelper.EnsureTable(bq.Name(), dataSchema)
	if err != nil {
		return err
	}

	if err := bq.schemaProcessor.ApplyDBTypingToObject(dbSchema, fact); err != nil {
		return err
	}

	return bq.bqAdapter.Insert(dataSchema, fact)
}

//Store call StoreWithParseFunc with parsers.ParseJson func
func (bq *BigQuery) Store(fileName string, payload []byte) (int, error) {
	return bq.StoreWithParseFunc(fileName, payload, parsers.ParseJson)
}

//StoreWithParseFunc store file from byte payload to google cloud storage with processing
//return rowsCount and err if err occurred
//but return 0 and nil if no err
//because Store method doesn't store data to BigQuery(only to GCP)
func (bq *BigQuery) StoreWithParseFunc(fileName string, payload []byte, parseFunc func([]byte) (map[string]interface{}, error)) (int, error) {
	flatData, failedEvents, err := bq.schemaProcessor.ProcessFilePayload(fileName, payload, bq.breakOnError, parseFunc)
	if err != nil {
		return linesCount(payload), err
	}

	var rowsCount int
	for _, fdata := range flatData {
		rowsCount += fdata.GetPayloadLen()
	}

	//events cache
	defer func() {
		for _, fdata := range flatData {
			for _, object := range fdata.GetPayload() {
				if err != nil {
					bq.eventsCache.Error(bq.Name(), events.ExtractEventId(object), err.Error())
				} else {
					bq.eventsCache.Succeed(bq.Name(), events.ExtractEventId(object), object, fdata.DataSchema, bq.ColumnTypesMapping())
				}
			}
		}
	}()

	for _, fdata := range flatData {
		dbSchema, err := bq.tableHelper.EnsureTable(bq.Name(), fdata.DataSchema)
		if err != nil {
			return rowsCount, err
		}

		if err := bq.schemaProcessor.ApplyDBTyping(dbSchema, fdata); err != nil {
			return rowsCount, err
		}
	}

	for _, fdata := range flatData {
		b, fileRows := fdata.GetPayloadBytes(schema.JsonMarshallerInstance)
		err := bq.gcsAdapter.UploadBytes(buildDataIntoFileName(fdata, fileRows), b)
		if err != nil {
			return fileRows, err
		}
	}

	//send failed events to fallback only if other events have been inserted ok
	bq.Fallback(failedEvents...)
	counters.ErrorEvents(bq.Name(), len(failedEvents))
	for _, failedFact := range failedEvents {
		bq.eventsCache.Error(bq.Name(), failedFact.EventId, failedFact.Error)
	}

	return 0, nil
}

func (bq *BigQuery) SyncStore(objects []map[string]interface{}) (int, error) {
	return 0, errors.New("BigQuery doesn't support sync store")
}

//Fallback log event with error to fallback logger
func (bq *BigQuery) Fallback(failedFacts ...*events.FailedFact) {
	for _, failedFact := range failedFacts {
		bq.fallbackLogger.ConsumeAny(failedFact)
	}
}

func (bq *BigQuery) ColumnTypesMapping() map[typing.DataType]string {
	return adapters.SchemaToBigQueryString
}

func (bq *BigQuery) Name() string {
	return bq.name
}

func (bq *BigQuery) Type() string {
	return BigQueryType
}

func (bq *BigQuery) Close() (multiErr error) {
	bq.closed = true

	if bq.gcsAdapter != nil {
		if err := bq.gcsAdapter.Close(); err != nil {
			multiErr = multierror.Append(multiErr, fmt.Errorf("[%s] Error closing google cloud storage client: %v", bq.Name(), err))
		}
	}

	if err := bq.bqAdapter.Close(); err != nil {
		multiErr = multierror.Append(multiErr, fmt.Errorf("[%s] Error closing BigQuery client: %v", bq.Name(), err))
	}

	if bq.streamingWorker != nil {
		bq.streamingWorker.Close()
	}

	if err := bq.fallbackLogger.Close(); err != nil {
		multiErr = multierror.Append(multiErr, fmt.Errorf("[%s] Error closing fallback logger: %v", bq.Name(), err))
	}

	return
}
