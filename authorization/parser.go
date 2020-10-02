package authorization

import (
	"encoding/json"
	"errors"
	"fmt"
	"github.com/google/martian/log"
	"github.com/spf13/viper"
	"reflect"
	"strings"
)

type TokensPayload struct {
	Js  []interface{} `json:"js,omitempty"`
	Api []interface{} `json:"api,omitempty"`
}

//parse tokens from formats:
//{"js": value, "api": value} where value might be strings array or json objects array with object format:
//{"token":"123", "origins":["origin1", "origin2"]}
func parseFromBytes(b []byte) (map[string][]string, map[string][]string, error) {
	payload := &TokensPayload{}
	err := json.Unmarshal(b, payload)
	if err != nil {
		return nil, nil, fmt.Errorf("Error unmarshalling tokens. Payload must be json with 'js' and 'api' keys of json array or string array formats: %v", err)
	}

	jsTokens, err := reformatObj(payload.Js)
	if err != nil {
		return nil, nil, err
	}

	apiTokens, err := reformatObj(payload.Api)
	if err != nil {
		return nil, nil, err
	}

	return jsTokens, apiTokens, nil
}

//parse from viper string slice or from viper json string value
func parseFromConfig(viperConfig *viper.Viper, key string) (tokensOrigins map[string][]string) {
	tokensOrigins = map[string][]string{}

	tokensStrArr := viperConfig.GetStringSlice(key)
	if len(tokensStrArr) > 0 {
		for _, t := range tokensStrArr {
			trimmed := strings.TrimSpace(t)
			if trimmed != "" {
				tokensOrigins[trimmed] = []string{}
			}
		}
		return
	}

	jsonStr := viperConfig.GetString(key)
	if jsonStr == "" {
		return
	}

	var tokensArr []interface{}
	var err error
	if err = json.Unmarshal([]byte(jsonStr), &tokensArr); err != nil {
		log.Errorf("Error parsing [%s] tokens from config: %v", key, err)
		return
	}

	tokensOrigins, err = reformatObj(tokensArr)
	if err != nil {
		log.Errorf("Error parsing [%s] tokens from config: %v", key, err)
	}

	return
}

func reformatObj(tokensArr []interface{}) (map[string][]string, error) {
	tokensOrigins := map[string][]string{}
	for _, t := range tokensArr {
		switch t.(type) {
		case string:
			token := t.(string)
			trimmed := strings.TrimSpace(token)
			if trimmed != "" {
				tokensOrigins[trimmed] = []string{}
			}
		case map[string]interface{}:
			tokenObj := t.(map[string]interface{})
			token, ok := tokenObj["token"]
			if !ok {
				return nil, errors.New("Unknown authorization token format: each object must contain token field")
			}

			var origins []string
			trimmed := strings.TrimSpace(token.(string))
			if trimmed != "" {
				originsObj, ok := tokenObj["origins"]
				if ok {
					originsArr, ok := originsObj.([]interface{})
					if !ok {
						return nil, errors.New("Unknown authorization origins format: origins must be array of strings")
					}

					for _, originI := range originsArr {
						origins = append(origins, originI.(string))
					}
				}

				tokensOrigins[trimmed] = origins
			}
		default:
			return nil, errors.New("Unknown authorization token format type: " + reflect.TypeOf(t).Name())
		}

	}

	return tokensOrigins, nil
}
