package bandwidth

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/ioutil"
	"net/http"
	"net/url"
	"reflect"
	"strconv"
	"strings"
	"time"
)

// RateLimitError is error for 429 http error
type RateLimitError struct {
	Reset time.Time
}

func (e *RateLimitError) Error() string {
	return fmt.Sprintf("RateLimitError: reset at %v", e.Reset)
}

// Client is main API object
type Client struct {
	UserID, APIToken, APISecret string
	APIEndPoint                 string
	HTTPClient                  *http.Client
}

// New creates new instances of api
// It returns Client instance. Use it to make API calls.
// example: api := bandwidth.New("userId", "apiToken", "apiSecret")
func New(userID, apiToken, apiSecret string, other ...string) (*Client, error) {
	apiEndPoint := "https://api.catapult.inetwork.com"
	if userID == "" || apiToken == "" || apiSecret == "" {
		return nil, errors.New("Missing auth data. Please use api := bandwidth.New(\"user-id\", \"api-token\", \"api-secret\")")
	}
	l := len(other)
	if l > 0 {
		apiEndPoint = other[0]
	}
	client := &Client{userID, apiToken, apiSecret, apiEndPoint, http.DefaultClient}
	return client, nil
}

func (c *Client) concatUserPath(path string) string {
	if path[0] != '/' {
		path = "/" + path
	}
	return fmt.Sprintf("/users/%s%s", c.UserID, path)
}

func (c *Client) prepareURL(path string, version string) string {
	if path[0] != '/' {
		path = "/" + path
	}
	return fmt.Sprintf("%s/%s%s", c.APIEndPoint, version, path)
}

func (c *Client) createRequest(method, path string, version string) (*http.Request, error) {
	request, err := http.NewRequest(method, c.prepareURL(path, version), nil)
	if err != nil {
		return nil, err
	}
	request.SetBasicAuth(c.APIToken, c.APISecret)
	request.Header.Set("Accept", "application/json")
	request.Header.Set("User-Agent", fmt.Sprintf("go-bandwidth/v%s", Version))
	return request, nil
}

func (c *Client) checkResponse(response *http.Response, responseBody interface{}) (interface{}, http.Header, error) {
	defer response.Body.Close()
	body := responseBody
	if body == nil {
		body = map[string]interface{}{}
	}
	rawJSON, err := ioutil.ReadAll(response.Body)
	if err != nil {
		return nil, nil, err
	}
	if response.StatusCode >= 200 && response.StatusCode < 400 {
		if len(rawJSON) > 0 {
			err = json.Unmarshal([]byte(rawJSON), &body)
			if err != nil {
				return nil, nil, err
			}
		}
		return body, response.Header, nil
	}
	if response.StatusCode == 429 {
		reset, _ := strconv.ParseInt(response.Header.Get("X-RateLimit-Reset"), 10, 64)
		return nil, nil, &RateLimitError{Reset: time.Unix(int64((reset/1000)+1), 0)}
	}
	errorBody := make(map[string]interface{})
	if len(rawJSON) > 0 {
		err = json.Unmarshal([]byte(rawJSON), &errorBody)
		if err != nil {
			return nil, nil, err
		}
	}
	message := errorBody["message"]
	if message == nil {
		message = errorBody["code"]
	}
	if message == nil {
		return nil, nil, fmt.Errorf("Http code %d", response.StatusCode)
	}
	return nil, nil, errors.New(message.(string))
}

func (c *Client) makeRequestInternal(method, path string, version string, data ...interface{}) (interface{}, http.Header, error) {
	request, err := c.createRequest(method, path, version)
	var responseBody interface{}
	treatDataAsQuery := false
	if err != nil {
		return nil, nil, err
	}
	if len(data) > 0 {
		responseBody = data[0]
	}
	if len(data) > 2 {
		treatDataAsQuery = data[2].(bool)
	}
	if len(data) > 1 {
		if method == "GET" || treatDataAsQuery {
			var item map[string]string
			if data[1] == nil {
				item = make(map[string]string)
			} else {
				var ok bool
				item, ok = data[1].(map[string]string)
				if !ok {
					item = make(map[string]string)
					structType := reflect.TypeOf(data[1]).Elem()
					structValue := reflect.ValueOf(data[1])
					if !structValue.IsNil() {
						structValue = structValue.Elem()
						fieldCount := structType.NumField()
						for i := 0; i < fieldCount; i++ {
							fieldName := structType.Field(i).Name
							fieldValue := structValue.Field(i).Interface()
							if fieldValue == reflect.Zero(structType.Field(i).Type).Interface() {
								//ignore fields with default values
								continue
							}
							item[strings.Replace(strings.ToLower(string(fieldName[0]))+fieldName[1:], "ID", "Id", -1)] = fmt.Sprintf("%v", fieldValue)
						}
					}
				}
			}
			query := make(url.Values)
			for key, value := range item {
				query[key] = []string{value}
			}
			request.URL.RawQuery = query.Encode()
		} else {
			request.Header.Set("Content-Type", "application/json")
			rawJSON, err := json.Marshal(data[1])
			if err != nil {
				return nil, nil, err
			}
			request.Body = nopCloser{bytes.NewReader(rawJSON)}
		}
	}
	response, err := c.HTTPClient.Do(request)
	if err != nil {
		return nil, nil, err
	}
	return c.checkResponse(response, responseBody)
}

func (c *Client) makeRequest(method, path string, data ...interface{}) (interface{}, http.Header, error) {
	return c.makeRequestInternal(method, path, "v1", data...)
}

func (c *Client) makeRequestV2(method, path string, data ...interface{}) (interface{}, http.Header, error) {
	return c.makeRequestInternal(method, path, "v2", data...)
}

func getIDFromLocationHeader(headers http.Header) string {
	return getIDFromLocation(headers.Get("Location"))
}

func getIDFromLocation(location string) string {
	list := strings.Split(location, "/")
	l := len(list)
	if l == 0 {
		return ""
	}
	return list[l-1]
}

type nopCloser struct {
	io.Reader
}

func (nopCloser) Close() error { return nil }
