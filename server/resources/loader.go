package resources

import (
	"errors"
	"fmt"
	"github.com/jitsucom/jitsu/server/logging"
	"io/ioutil"
	"net/http"
	"net/url"
	"strings"
)

type ContentType string

const (
	lastModifiedHeader    = "Last-Modified"
	ifModifiedSinceHeader = "If-Modified-Since"

	JsonContentType    ContentType = "json"
	YamlContentType    ContentType = "yaml"
	UnknownContentType ContentType = "unknown"
)

var ErrNoModified = errors.New("Resource wasn't modified")

type ResponsePayload struct {
	Content      []byte
	LastModified string

	ContentType *ContentType
}

//return loaded content, empty string (because there is no last-modified logic in files), error
func LoadFromFile(filePath, lastModified string) (*ResponsePayload, error) {
	filePath = strings.ReplaceAll(filePath, "file://", "")

	b, err := ioutil.ReadFile(filePath)
	if err != nil {
		return nil, fmt.Errorf("Error loading resource from file %s: %v", filePath, err)
	}

	var contentType ContentType
	if strings.HasSuffix(filePath, ".yaml") {
		contentType = YamlContentType
	} else if strings.HasSuffix(filePath, ".json") {
		contentType = JsonContentType
	} else {
		logging.Errorf("Unknown content type in config file: %s", filePath)
		contentType = UnknownContentType
	}

	return &ResponsePayload{Content: b, ContentType: &contentType}, nil
}

//return loaded content, Last-modified value from header, error
func LoadFromHttp(fullUrl, ifModifiedSinceValue string) (*ResponsePayload, error) {
	var username, password string
	if strings.Contains(fullUrl, "@") {
		parsedUrl, err := url.Parse(fullUrl)
		if err != nil {
			return nil, err
		}

		if parsedUrl.User != nil {
			username = parsedUrl.User.Username()
			pass, ok := parsedUrl.User.Password()
			if ok {
				password = pass
			}
		}

		urlParts := strings.Split(fullUrl, "@")
		if strings.HasPrefix(fullUrl, "https:") {
			fullUrl = "https://" + urlParts[1]
		} else {
			fullUrl = "http://" + urlParts[1]
		}
	}

	req, err := http.NewRequest(http.MethodGet, fullUrl, nil)
	if err != nil {
		return nil, err
	}

	if username != "" {
		req.SetBasicAuth(username, password)
	}

	req.Header.Add(ifModifiedSinceHeader, ifModifiedSinceValue)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("Error loading resource from url %s: %v", fullUrl, err)
	}
	defer resp.Body.Close()

	if resp.StatusCode == 304 {
		return nil, ErrNoModified
	}

	if resp.StatusCode != 200 {
		return nil, fmt.Errorf("Error loading resource from url %s: http code isn't 200 [%d]", fullUrl, resp.StatusCode)
	}

	b, err := ioutil.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("Error reading resource from url %s: %v", fullUrl, err)
	}

	httpContentType := resp.Header.Get("Content-Type")

	var contentType ContentType
	if strings.Contains(httpContentType, "yaml") {
		contentType = YamlContentType
	} else if strings.Contains(httpContentType, "json") {
		contentType = JsonContentType
	} else {
		logging.Errorf("Unknown content type [%s] in response from url: %s", httpContentType, fullUrl)
		contentType = UnknownContentType
	}

	return &ResponsePayload{
		Content:      b,
		LastModified: resp.Header.Get(lastModifiedHeader),
		ContentType:  &contentType,
	}, nil
}
