package handlers

import (
	"context"
	"errors"
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"
	"github.com/hashicorp/go-multierror"
	"github.com/jitsucom/jitsu/server/adapters"
	"github.com/jitsucom/jitsu/server/logging"
	"github.com/jitsucom/jitsu/server/middleware"
	"github.com/jitsucom/jitsu/server/storages"
)

func DestinationsHandler(c *gin.Context) {
	destinationConfig := &storages.DestinationConfig{}
	if err := c.BindJSON(destinationConfig); err != nil {
		logging.Error("Error parsing destinations body: %v", err)
		c.JSON(http.StatusBadRequest, middleware.ErrorResponse{Message: "Failed to parse body", Error: err.Error()})
		return
	}
	err := testConnection(destinationConfig)
	if err != nil {
		c.JSON(http.StatusBadRequest, middleware.ErrorResponse{Message: err.Error()})
		return
	}
	c.Status(http.StatusOK)
}

func testConnection(config *storages.DestinationConfig) error {
	switch config.Type {
	case storages.PostgresType:
		if err := config.DataSource.Validate(); err != nil {
			return err
		}

		postgres, err := adapters.NewPostgres(context.Background(), config.DataSource, nil, map[string]string{})
		if err != nil {
			return err
		}

		postgres.Close()
		return nil
	case storages.ClickHouseType:
		if err := config.ClickHouse.Validate(); err != nil {
			return err
		}

		var multiErr error
		for _, dsn := range config.ClickHouse.Dsns {
			ch, err := adapters.NewClickHouse(context.Background(), strings.TrimSpace(dsn),
				"", "", nil, nil, nil, nil, map[string]string{})
			if err != nil {
				multiErr = multierror.Append(multiErr, err)
				continue
			} else {
				ch.Close()
			}
		}
		return multiErr
	case storages.RedshiftType:
		if err := config.DataSource.Validate(); err != nil {
			return err
		}

		if config.Mode == storages.BatchMode {
			if err := config.S3.Validate(); err != nil {
				return err
			}
			s3, err := adapters.NewS3(config.S3)
			if err != nil {
				return err
			}
			defer s3.Close()
			if err = s3.ValidateWritePermission(); err != nil {
				return err
			}
		}

		redshift, err := adapters.NewAwsRedshift(context.Background(), config.DataSource, config.S3, nil, map[string]string{})
		if err != nil {
			return err
		}

		redshift.Close()
		return nil
	case storages.BigQueryType:
		if err := config.Google.Validate(config.Mode != storages.BatchMode); err != nil {
			return err
		}

		bq, err := adapters.NewBigQuery(context.Background(), config.Google, nil, map[string]string{})
		if err != nil {
			return err
		}
		defer bq.Close()
		if config.Mode == storages.BatchMode {
			googleStorage, err := adapters.NewGoogleCloudStorage(context.Background(), config.Google)
			if err != nil {
				return err
			}
			defer googleStorage.Close()
			if err = googleStorage.ValidateWritePermission(); err != nil {
				return nil
			}
		}
		return bq.Test()
	case storages.SnowflakeType:
		if err := config.Snowflake.Validate(); err != nil {
			return err
		}
		snowflake, err := adapters.NewSnowflake(context.Background(), config.Snowflake, nil, nil, map[string]string{})
		if err != nil {
			return err
		}
		defer snowflake.Close()
		if config.Mode == storages.BatchMode {
			if config.S3 != nil && config.S3.Bucket != "" {
				if err := config.S3.Validate(); err != nil {
					return err
				}
				s3, err := adapters.NewS3(config.S3)
				if err != nil {
					return err
				}
				defer s3.Close()
				if err = s3.ValidateWritePermission(); err != nil {
					return err
				}
			} else if config.Google != nil && config.Google.Bucket != "" {
				if err := config.Google.Validate(false); err != nil {
					return err
				}
				gcp, err := adapters.NewGoogleCloudStorage(context.Background(), config.Google)
				if err != nil {
					return err
				}
				defer gcp.Close()
				if err = gcp.ValidateWritePermission(); err != nil {
					return err
				}
			}
		}
		return nil
	case storages.GoogleAnalyticsType:
		if err := config.GoogleAnalytics.Validate(); err != nil {
			return err
		}

		return nil
	case storages.FacebookType:
		if err := config.Facebook.Validate(); err != nil {
			return err
		}

		adapter := adapters.NewFacebookConversion(config.Facebook, nil)
		defer adapter.Close()

		if err := adapter.TestAccess(); err != nil {
			return err
		}

		return nil
	default:
		return errors.New("unsupported destination type " + config.Type)
	}
}
