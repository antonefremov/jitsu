package fallback

import (
	"errors"
	"fmt"
	"github.com/hashicorp/go-multierror"
	"github.com/jitsucom/jitsu/server/destinations"
	"github.com/jitsucom/jitsu/server/logfiles"
	"github.com/jitsucom/jitsu/server/logging"
	"github.com/jitsucom/jitsu/server/metrics"
	"github.com/jitsucom/jitsu/server/parsers"
	"io/ioutil"
	"os"
	"path"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
)

const (
	fallbackFileMaskPostfix = "failed.dst=*-20*.log"
	fallbackIdentifier      = "fallback"
)

var destinationIDExtractRegexp = regexp.MustCompile("failed.dst=(.*)-\\d\\d\\d\\d-\\d\\d-\\d\\dT")

type Service struct {
	fallbackDir        string
	fileMask           string
	statusManager      *logfiles.StatusManager
	destinationService *destinations.Service
	archiver           *logfiles.Archiver

	locks sync.Map
}

//only for tests
func NewTestService() *Service {
	return &Service{}
}

func NewService(logEventsPath string, destinationService *destinations.Service) (*Service, error) {
	fallbackPath := path.Join(logEventsPath, logging.FailedDir)
	logArchiveEventPath := path.Join(logEventsPath, logging.ArchiveDir)
	statusManager, err := logfiles.NewStatusManager(fallbackPath)
	if err != nil {
		return nil, fmt.Errorf("Error creating fallback files status manager: %v", err)
	}
	return &Service{
		fallbackDir:        fallbackPath,
		statusManager:      statusManager,
		fileMask:           path.Join(fallbackPath, fallbackFileMaskPostfix),
		destinationService: destinationService,
		archiver:           logfiles.NewArchiver(fallbackPath, logArchiveEventPath),
	}, nil
}

func (s *Service) Replay(fileName, destinationID string, rawFile bool) error {
	if fileName == "" {
		return errors.New("File name can't be empty")
	}

	//handle absolute and local path
	var filePath string
	if strings.HasPrefix(fileName, "/") {
		filePath = fileName
		fileName = filepath.Base(fileName)
	} else {
		filePath = path.Join(s.fallbackDir, fileName)
	}

	_, loaded := s.locks.LoadOrStore(fileName, true)
	if loaded {
		return fmt.Errorf("File [%s] is being processed", fileName)
	}
	defer s.locks.Delete(fileName)

	b, err := ioutil.ReadFile(filePath)
	if err != nil {
		return fmt.Errorf("Error reading fallback file [%s]: %v", fileName, err)
	}

	if destinationID == "" {
		//get destinationID from filename
		regexResult := destinationIDExtractRegexp.FindStringSubmatch(fileName)
		if len(regexResult) != 2 {
			return fmt.Errorf("Error processing fallback file %s: Malformed name", fileName)
		}

		destinationID = regexResult[1]
	}

	storageProxy, ok := s.destinationService.GetStorageByID(destinationID)
	if !ok {
		return fmt.Errorf("Destination [%s] wasn't found", destinationID)
	}

	storage, ok := storageProxy.Get()
	if !ok {
		return fmt.Errorf("Destination [%s] hasn't been initialized yet", destinationID)
	}
	if storage.IsStaging() {
		return fmt.Errorf("Error running fallback for destination [%s] in staged mode, "+
			"cannot be used to store data (only available for dry-run)", destinationID)
	}

	alreadyUploadedTables := map[string]bool{}
	tableStatuses := s.statusManager.GetTablesStatuses(fileName, storage.Name())
	for tableName, status := range tableStatuses {
		if status.Uploaded {
			alreadyUploadedTables[tableName] = true
		}
	}

	parserFunc := parsers.ParseFallbackJSON
	if rawFile {
		parserFunc = parsers.ParseJSON
	}

	resultPerTable, errRowsCount, err := storage.StoreWithParseFunc(fileName, b, alreadyUploadedTables, parserFunc)
	if errRowsCount > 0 {
		metrics.ErrorTokenEvents(fallbackIdentifier, storage.Name(), errRowsCount)
	}

	if err != nil {
		return fmt.Errorf("[%s] Error storing fallback file %s in destination: %v", storage.Name(), fileName, err)
	}

	var multiErr error
	for tableName, result := range resultPerTable {
		if result.Err != nil {
			multiErr = multierror.Append(multiErr, result.Err)
			logging.Errorf("[%s] Error storing table %s from file %s: %v", storage.Name(), tableName, filePath, result.Err)
			metrics.ErrorTokenEvents(fallbackIdentifier, storage.Name(), result.RowsCount)
		} else {
			metrics.SuccessTokenEvents(fallbackIdentifier, storage.Name(), result.RowsCount)
		}

		s.statusManager.UpdateStatus(fileName, storage.Name(), tableName, result.Err)
	}

	if multiErr == nil {
		archiveErr := s.archiver.ArchiveByPath(filePath)
		if archiveErr != nil {
			logging.SystemErrorf("Error archiving [%s] fallback file: %v", filePath, err)
		} else {
			s.statusManager.CleanUp(fileName)
		}

		return nil
	} else {
		return multiErr
	}
}

func (s *Service) GetFileStatuses(destinationsFilter map[string]bool) []*FileStatus {
	files, err := filepath.Glob(s.fileMask)
	if err != nil {
		logging.Errorf("Error finding fallback files by mask [%s]: %v", s.fileMask, err)
		return []*FileStatus{}
	}

	fileStatuses := []*FileStatus{}

	for _, filePath := range files {
		fileName := filepath.Base(filePath)

		b, err := ioutil.ReadFile(filePath)
		if err != nil {
			logging.Errorf("Error reading fallback file [%s]: %v", filePath, err)
			continue
		}
		if len(b) == 0 {
			os.Remove(filePath)
			s.statusManager.CleanUp(fileName)
			continue
		}

		//get destinationID from filename
		regexResult := destinationIDExtractRegexp.FindStringSubmatch(fileName)
		if len(regexResult) != 2 {
			logging.Errorf("Error processing fallback file %s. Malformed name", filePath)
			continue
		}

		destinationID := regexResult[1]
		_, ok := destinationsFilter[destinationID]
		if len(destinationsFilter) > 0 && !ok {
			continue
		}

		statuses := s.statusManager.GetTablesStatuses(fileName, destinationID)

		fileStatuses = append(fileStatuses, &FileStatus{
			FileName:      fileName,
			DestinationID: destinationID,
			TablesStatus:  statuses,
		})

	}

	return fileStatuses
}
