package minideploy

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"strconv"
	"strings"
)

const (
	DefaultURL        = "http://127.0.0.1:9000"
	responseBodyLimit = 64 << 10
)

type Deployment struct {
	App              string `json:"app"`
	Supported        bool   `json:"supported"`
	Status           string `json:"status"`
	DatabaseAttached bool   `json:"databaseAttached"`
	DatabaseDetached bool   `json:"databaseDetached"`
	DatabaseID       string `json:"databaseId,omitempty"`
}

type HTTPError struct {
	Status int
}

func (err HTTPError) Error() string {
	return fmt.Sprintf(
		"MiniDeploy request failed with HTTP %d",
		err.Status,
	)
}

type Client struct {
	baseURL *url.URL
	token   []byte
	client  *http.Client
}

func NewClient(
	rawURL string,
	token []byte,
	client *http.Client,
) (*Client, error) {
	parsed, err := url.Parse(rawURL)
	if err != nil ||
		parsed.Scheme != "http" ||
		parsed.User != nil ||
		parsed.RawQuery != "" ||
		parsed.Fragment != "" ||
		(parsed.Path != "" && parsed.Path != "/") {

		return nil, fmt.Errorf(
			"invalid MiniDeploy lifecycle URL",
		)
	}

	host, portText, err := net.SplitHostPort(
		parsed.Host,
	)
	if err != nil {
		return nil, fmt.Errorf(
			"invalid MiniDeploy lifecycle URL",
		)
	}

	ip := net.ParseIP(host)
	port, portErr := strconv.Atoi(portText)

	if ip == nil ||
		!ip.IsLoopback() ||
		portErr != nil ||
		port < 1 ||
		port > 65535 {

		return nil, fmt.Errorf(
			"MiniDeploy lifecycle URL must use loopback",
		)
	}

	copiedToken := append([]byte(nil), token...)
	if len(copiedToken) < 32 ||
		strings.ContainsAny(
			string(copiedToken),
			" \t\r\n",
		) {

		return nil, fmt.Errorf(
			"invalid MiniDeploy integration token",
		)
	}

	if client == nil {
		client = &http.Client{}
	}

	return &Client{
		baseURL: parsed,
		token:   copiedToken,
		client:  client,
	}, nil
}

func (client *Client) endpoint(
	endpointPath string,
) string {
	resolved := *client.baseURL
	resolved.Path = endpointPath
	return resolved.String()
}

func (client *Client) do(
	ctx context.Context,
	method string,
	endpointPath string,
	input any,
	output any,
) error {
	var body io.Reader

	if input != nil {
		content, err := json.Marshal(input)
		if err != nil {
			return fmt.Errorf(
				"encode MiniDeploy lifecycle request",
			)
		}
		body = bytes.NewReader(content)
	}

	request, err := http.NewRequestWithContext(
		ctx,
		method,
		client.endpoint(endpointPath),
		body,
	)
	if err != nil {
		return fmt.Errorf(
			"prepare MiniDeploy lifecycle request",
		)
	}

	request.Header.Set(
		"Authorization",
		"Bearer "+string(client.token),
	)
	request.Header.Set(
		"Accept",
		"application/json",
	)

	if input != nil {
		request.Header.Set(
			"Content-Type",
			"application/json",
		)
	}

	response, err := client.client.Do(request)
	if err != nil {
		return fmt.Errorf(
			"MiniDeploy is unavailable",
		)
	}
	defer response.Body.Close()

	content, err := io.ReadAll(
		io.LimitReader(
			response.Body,
			responseBodyLimit+1,
		),
	)
	if err != nil {
		return fmt.Errorf(
			"read MiniDeploy response",
		)
	}

	if len(content) > responseBodyLimit {
		return fmt.Errorf(
			"MiniDeploy response exceeded safety limit",
		)
	}

	if response.StatusCode < 200 ||
		response.StatusCode >= 300 {

		return HTTPError{
			Status: response.StatusCode,
		}
	}

	if output == nil ||
		response.StatusCode == http.StatusNoContent {

		return nil
	}

	decoder := json.NewDecoder(
		bytes.NewReader(content),
	)
	decoder.DisallowUnknownFields()

	if err := decoder.Decode(output); err != nil {
		return fmt.Errorf(
			"MiniDeploy returned invalid response",
		)
	}

	if err := decoder.Decode(
		&struct{}{},
	); !errors.Is(err, io.EOF) {

		return fmt.Errorf(
			"MiniDeploy returned invalid response",
		)
	}

	return nil
}

func (client *Client) ListDeployments(
	ctx context.Context,
) ([]Deployment, error) {
	var deployments []Deployment

	if err := client.do(
		ctx,
		http.MethodGet,
		"/internal/minibase/deployments",
		nil,
		&deployments,
	); err != nil {
		return nil, err
	}

	if deployments == nil {
		deployments = make([]Deployment, 0)
	}

	return deployments, nil
}

func (client *Client) DetachDatabase(
	ctx context.Context,
	app string,
	databaseID string,
	attachmentID string,
) error {
	return client.do(
		ctx,
		http.MethodPost,
		"/internal/minibase/deployments/"+
			url.PathEscape(app)+
			"/database/detach",
		struct {
			DatabaseID   string `json:"databaseId"`
			AttachmentID string `json:"attachmentId"`
		}{
			DatabaseID:   databaseID,
			AttachmentID: attachmentID,
		},
		nil,
	)
}

func (client *Client) AttachDatabase(
	ctx context.Context,
	app string,
	databaseID string,
) error {
	return client.do(
		ctx,
		http.MethodPost,
		"/internal/minibase/deployments/"+
			url.PathEscape(app)+
			"/database/attach",
		struct {
			DatabaseID string `json:"databaseId"`
		}{
			DatabaseID: databaseID,
		},
		nil,
	)
}
