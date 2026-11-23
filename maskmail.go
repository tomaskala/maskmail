package maskmail

import (
	"bytes"
	"context"
	"encoding/json/jsontext"
	"encoding/json/v2"
	"fmt"
	"net/http"
	"time"
)

const (
	sessionURL     = "https://api.fastmail.com/jmap/session"
	capCore        = "urn:ietf:params:jmap:core"
	capMaskedEmail = "https://www.fastmail.com/dev/maskedemail"
)

type jmapInvocation [3]jsontext.Value

type jmapRequest struct {
	Using       []string         `json:"using"`
	MethodCalls []jmapInvocation `json:"methodCalls"`
}

type jmapResponse struct {
	SessionState    string           `json:"sessionState"`
	MethodResponses []jmapInvocation `json:"methodResponses"`
}

type ID string

type Account struct {
	Name                string         `json:"name"`
	IsPersonal          bool           `json:"isPersonal"`
	IsReadOnly          bool           `json:"isReadOnly"`
	AccountCapabilities map[string]any `json:"accountCapabilities"`
}

type Session struct {
	Capabilities    map[string]any `json:"capabilities"`
	Accounts        map[ID]Account `json:"accounts"`
	PrimaryAccounts map[string]ID  `json:"primaryAccounts"`
	Username        string         `json:"username"`
	APIURL          string         `json:"apiUrl"`
	DownloadURL     string         `json:"downloadUrl"`
	UploadURL       string         `json:"uploadUrl"`
	EventSourceURL  string         `json:"eventSourceUrl"`
	State           string         `json:"state"`
}

type MaskedEmailState string

const (
	StatePending  MaskedEmailState = "pending"
	StateEnabled  MaskedEmailState = "enabled"
	StateDisabled MaskedEmailState = "disabled"
	StateDeleted  MaskedEmailState = "deleted"
)

type MaskedEmail struct {
	ID            string           `json:"id,omitzero"`
	Email         string           `json:"email,omitzero"`
	State         MaskedEmailState `json:"state,omitzero"`
	ForDomain     string           `json:"forDomain,omitzero"`
	Description   string           `json:"description,omitzero"`
	LastMessageAt *time.Time       `json:"lastMessageAt,omitzero"`
	CreatedAt     *time.Time       `json:"createdAt,omitzero"`
	CreatedBy     string           `json:"createdBy,omitzero"`
	URL           *string          `json:"url,omitzero"`
	EmailPrefix   string           `json:"emailPrefix,omitzero"`
}

type SetRequest struct {
	AccountID ID                 `json:"accountId"`
	IfInState *string            `json:"ifInState,omitzero"`
	Create    map[ID]MaskedEmail `json:"create,omitempty"`
	Update    map[ID][]string    `json:"update,omitempty"`
	Destroy   []ID               `json:"destroy,omitempty"`
}

type SetError struct {
	Type        string  `json:"type"`
	Description *string `json:"description"`
}

type SetResponse struct {
	AccountID    ID                  `json:"accountId"`
	OldState     *string             `json:"oldState"`
	NewState     *string             `json:"newState"`
	Created      map[ID]MaskedEmail  `json:"created"`
	Updated      map[ID]*MaskedEmail `json:"updated"`
	Destroyed    []ID                `json:"destroyed"`
	NotCreated   map[ID]SetError     `json:"notCreated"`
	NotUpdated   map[ID]SetError     `json:"notUpdated"`
	NotDestroyed map[ID]SetError     `json:"notDestroyed"`
}

type GetRequest struct {
	AccountID  ID       `json:"accountId"`
	IDs        []ID     `json:"ids,omitempty"`
	Properties []string `json:"properties,omitempty"`
}

type GetResponse struct {
	AccountID    ID            `json:"accountId"`
	State        string        `json:"state"`
	MaskedEmails []MaskedEmail `json:"list"`
	NotFound     []ID          `json:"notFound"`
}

type Client struct {
	apiToken   string
	session    *Session
	httpClient *http.Client
}

func OpenSession(ctx context.Context, apiToken string, httpClient *http.Client) (*Client, error) {
	c := &Client{
		apiToken:   apiToken,
		httpClient: httpClient,
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, sessionURL, nil)
	if err != nil {
		return nil, fmt.Errorf("error creating HTTP request: %w", err)
	}
	req.Header = c.makeHeaders()

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("error getting HTTP response: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("returned status %d", resp.StatusCode)
	}

	if err := json.UnmarshalRead(resp.Body, &c.session); err != nil {
		return nil, fmt.Errorf("error unmarshalling HTTP response: %w", err)
	}
	return c, nil
}

func (c *Client) makeHeaders() http.Header {
	h := http.Header{}
	h.Set("Authorization", "Bearer "+c.apiToken)
	h.Set("Content-Type", "application/json; charset=utf-8")
	return h
}

func newJMAPInvocation(methodName string, args any, callID string) (jmapInvocation, error) {
	methodNameJSON, err := json.Marshal(methodName)
	if err != nil {
		return jmapInvocation{}, fmt.Errorf("error marshalling method name: %w", err)
	}

	argsJSON, err := json.Marshal(args)
	if err != nil {
		return jmapInvocation{}, fmt.Errorf("error marshalling args: %w", err)
	}

	callIDJSON, err := json.Marshal(callID)
	if err != nil {
		return jmapInvocation{}, fmt.Errorf("error marshalling call ID: %w", err)
	}

	return jmapInvocation{methodNameJSON, argsJSON, callIDJSON}, nil
}

func (c *Client) sendRequest(ctx context.Context, method string, args any) (jsontext.Value, error) {
	call, err := newJMAPInvocation(method, args, "a")
	if err != nil {
		return nil, fmt.Errorf("error building JMAP invocation: %w", err)
	}

	jmapRequest := jmapRequest{
		Using:       []string{capCore, capMaskedEmail},
		MethodCalls: []jmapInvocation{call},
	}
	body, err := json.Marshal(jmapRequest)
	if err != nil {
		return nil, fmt.Errorf("error marshalling JMAP request: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.session.APIURL, bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("error creating HTTP request: %w", err)
	}
	req.Header = c.makeHeaders()

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("error getting HTTP response: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("returned status %d", resp.StatusCode)
	}

	var jmapResponse jmapResponse
	if err := json.UnmarshalRead(resp.Body, &jmapResponse); err != nil {
		return nil, fmt.Errorf("error unmarshalling HTTP response: %w", err)
	}
	if len(jmapResponse.MethodResponses) == 0 {
		return nil, fmt.Errorf("no method responses in API reply")
	}

	return jmapResponse.MethodResponses[0][1], nil
}

func (c *Client) AccountID() (ID, bool) {
	accountID, ok := c.session.PrimaryAccounts[capMaskedEmail]
	return accountID, ok
}

func (c *Client) SendSetRequest(ctx context.Context, request SetRequest) (*SetResponse, error) {
	res, err := c.sendRequest(ctx, "MaskedEmail/set", request)
	if err != nil {
		return nil, fmt.Errorf("error sending set request: %w", err)
	}

	var response SetResponse
	if err := json.Unmarshal(res, &response); err != nil {
		return nil, fmt.Errorf("error unmarshalling set response: %w", err)
	}
	return &response, nil
}

func (c *Client) SendGetRequest(ctx context.Context, request GetRequest) (*GetResponse, error) {
	res, err := c.sendRequest(ctx, "MaskedEmail/get", request)
	if err != nil {
		return nil, fmt.Errorf("error sending get request: %w", err)
	}

	var response GetResponse
	if err := json.Unmarshal(res, &response); err != nil {
		return nil, fmt.Errorf("error unmarshalling get response: %w", err)
	}
	return &response, nil
}
