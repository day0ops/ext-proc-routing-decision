package test

import (
	"bytes"
	"cmp"
	"encoding/json"
	"fmt"
	"html/template"
	"io"
	"net/http"
	"regexp"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
	"gopkg.in/yaml.v3"
)

const DefaultURL = "http://127.0.0.1:10000"

type TestCases []Case

type Case struct {
	Name   string `yaml:"name"`
	Input  Input  `yaml:"input"`
	Expect Expect `yaml:"expect"`
	url    string
}

type Options interface {
	apply(*Case)
}

type optionFunc func(*Case)

func (f optionFunc) apply(v *Case) {
	f(v)
}

func WithURL(url string) Options {
	return optionFunc(func(c *Case) {
		c.url = url
	})
}

func (c Case) Run(t *testing.T, opts ...Options) {
	for _, opt := range opts {
		opt.apply(&c)
	}
	t.Run(c.Name, func(t *testing.T) {
		var err error
		got := httpCall(t, c)
		err = c.Expect.Assert(t, got)
		require.NoError(t, err)
	})
}

type Input struct {
	Headers Headers `yaml:"headers"`
}

type Headers []HeaderValue

func (headers Headers) Get(key string) string {
	for _, h := range headers {
		if h.Key == key {
			return h.Value
		}
	}
	return ""
}

type Actual struct {
	RequestHeaders  http.Header
	ResponseHeaders http.Header
}

type Expect struct {
	RequestHeaders  []HeaderMatch `yaml:"requestHeaders"`
	ResponseHeaders []HeaderMatch `yaml:"responseHeaders"`
}

func (e Expect) Assert(t *testing.T, actual Actual) error {
	for _, h := range e.RequestHeaders {
		if !h.Assert(t, actual.RequestHeaders) {
			return fmt.Errorf("header match fail: request header %q should match %q header values with %q=%q and its values are %v", h.Name, cmp.Or(h.MatchAction, MatchActionFirst), h.MatchType(), h.MatchValue(), actual.RequestHeaders.Values(h.Name))
		}
	}

	for _, h := range e.ResponseHeaders {
		if !h.Assert(t, actual.ResponseHeaders) {
			return fmt.Errorf("header match fail: response header %q should match %q header values with %q=%q and its values are %q", h.Name, cmp.Or(h.MatchAction, MatchActionFirst), h.MatchType(), h.MatchValue(), actual.ResponseHeaders.Values(h.Name))
		}
	}
	return nil
}

type HeaderValue struct {
	Key   string `yaml:"name"`
	Value string `yaml:"value"`
}

type MatchAction string

const (
	MatchActionFirst MatchAction = "FIRST"
	MatchActionAny   MatchAction = "ANY"
	MatchActionAll   MatchAction = "ALL"
)

type HeaderMatch struct {
	Name        string      `yaml:"name"`
	Exact       string      `yaml:"exact"`
	Absent      bool        `yaml:"absent"`
	Regex       string      `yaml:"regex"`
	MatchAction MatchAction `yaml:"matchAction"`
}

func (hm HeaderMatch) Assert(t *testing.T, headers http.Header) bool {
	switch hm.MatchAction {
	case "", MatchActionFirst:
		headerValue := headers.Get(hm.Name)
		return hm.match(headerValue)
	case MatchActionAny:
		for _, value := range headers.Values(hm.Name) {
			if hm.match(value) {
				return true
			}
		}
		return false
	case MatchActionAll:
		headerValues := headers.Values(hm.Name)
		if len(headerValues) == 0 {
			return false
		}
		for _, value := range headers.Values(hm.Name) {
			if !hm.match(value) {
				return false
			}
		}
		return true
	}
	return false
}

func (hm *HeaderMatch) match(value string) bool {
	switch {
	case hm.Absent:
		if hm.Absent {
			return value == ""
		}
		return value != ""
	case hm.Exact != "":
		return value == hm.Exact
	case hm.Regex != "":
		r := regexp.MustCompile(hm.Regex)
		return r.MatchString(value)
	}
	return false
}

func (hm *HeaderMatch) MatchType() string {
	switch {
	case hm.Exact != "":
		return "exact"
	case hm.Absent:
		return "absent"
	case hm.Regex != "":
		return "regex"
	}
	return ""
}

func (hm *HeaderMatch) MatchValue() string {
	switch {
	case hm.Exact != "":
		return hm.Exact
	case hm.Absent:
		return fmt.Sprintf("%t", hm.Absent)
	case hm.Regex != "":
		return hm.Regex
	}
	return ""
}

func (cases TestCases) Run(t *testing.T, opts ...Options) {
	for _, tt := range cases {
		tt.Run(t, opts...)
	}
}

func httpCall(t *testing.T, tt Case) Actual {
	httpClient := &http.Client{
		CheckRedirect: func(_ *http.Request, _ []*http.Request) error {
			return http.ErrUseLastResponse
		},
	}
	defer httpClient.CloseIdleConnections()

	baseURL := cmp.Or(tt.url, DefaultURL)
	u := fmt.Sprintf("%s%s", baseURL, tt.Input.Headers.Get("path"))
	req, err := http.NewRequest(http.MethodGet, u, nil)
	require.NoError(t, err)

	for _, header := range tt.Input.Headers {
		if strings.ToLower(header.Key) == "host" {
			req.Host = header.Value
		}
		if strings.ToLower(header.Key) == "method" {
			req.Method = header.Value
		}
		req.Header.Add(header.Key, header.Value)
	}
	res, err := httpClient.Do(req)
	require.NoError(t, err)
	defer res.Body.Close()
	require.True(t, (res.StatusCode > 200 || res.StatusCode < 499), "invalid status code in res from server", "status", res.StatusCode)

	var response struct {
		Headers map[string]string `json:"headers"`
	}
	body, err := io.ReadAll(res.Body)
	require.NoError(t, err)

	requestHeaders := http.Header{}
	if res.StatusCode == 200 && tt.Expect.RequestHeaders != nil {
		rdr := io.NopCloser(bytes.NewBuffer(body))
		err = json.NewDecoder(rdr).Decode(&response)
		require.NoError(t, err, "error decoding response")

		for k, v := range response.Headers {
			requestHeaders.Add(k, v)
		}
	}
	res.Header.Add("status", fmt.Sprintf("%d", res.StatusCode))
	actual := Actual{
		ResponseHeaders: res.Header,
		RequestHeaders:  requestHeaders,
	}

	return actual
}

func LoadTemplate(t *testing.T, path string, templateData any) TestCases {
	if testing.Short() {
		t.Skip()
	}
	return testData(t, templateData, path)
}

func testData(t *testing.T, templateData any, files ...string) TestCases {
	var configs TestCases
	for _, fileName := range files {
		if !strings.Contains(fileName, "testdata/") {
			fileName = fmt.Sprintf("testdata/%s", fileName)
		}

		tmpl, err := template.ParseFiles(fileName)
		require.NoError(t, err)
		b := bytes.NewBuffer([]byte{})
		err = tmpl.Execute(b, templateData)
		require.NoError(t, err)

		for _, doc := range bytes.Split(b.Bytes(), []byte("---")) {
			var testcase Case
			err = yaml.Unmarshal(doc, &testcase)
			require.NoError(t, err)
			configs = append(configs, testcase)
		}
	}

	return configs
}
