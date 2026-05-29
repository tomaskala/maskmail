package main

import (
	"bytes"
	"encoding/json"
	"flag"
	"fmt"
	"net/http"
	"os"
	"os/user"
	"text/tabwriter"
	"time"

	"github.com/zalando/go-keyring"
)

const (
	sessionURL     = "https://api.fastmail.com/jmap/session"
	capCore        = "urn:ietf:params:jmap:core"
	capMaskedEmail = "https://www.fastmail.com/dev/maskedemail"

	service = "maskmail"
	timeout = 5 * time.Second

	usage = `maskmail <create|show> [OPTIONS]

OPTIONS for create
      -domain       Domain this masked email is for (required)
      -description  Description of the masked email's usage (required)

OPTIONS for show
      -state  Only show masked emails in this state (all/pending/enabled/disabled/deleted)`
)

var (
	createFlag  = flag.NewFlagSet("create", flag.ContinueOnError)
	domain      = createFlag.String("domain", "", "Domain this masked email is for")
	description = createFlag.String("description", "", "Description of the masked email's usage")

	showFlag = flag.NewFlagSet("show", flag.ContinueOnError)
	state    = showFlag.String("state", "all", "Only show masked emails in this state (all/pending/enabled/disabled/deleted)")
)

type JMAPInvocation [3]json.RawMessage

type JMAPRequest struct {
	Using       []string         `json:"using"`
	MethodCalls []JMAPInvocation `json:"methodCalls"`
}

type JMAPResponse struct {
	SessionState    string           `json:"sessionState"`
	MethodResponses []JMAPInvocation `json:"methodResponses"`
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
	ID            string           `json:"id,omitempty"`
	Email         string           `json:"email,omitempty"`
	State         MaskedEmailState `json:"state,omitempty"`
	ForDomain     string           `json:"forDomain,omitempty"`
	Description   string           `json:"description,omitempty"`
	LastMessageAt *time.Time       `json:"lastMessageAt,omitempty"`
	CreatedAt     *time.Time       `json:"createdAt,omitempty"`
	CreatedBy     string           `json:"createdBy,omitempty"`
	URL           *string          `json:"url,omitempty"`
	EmailPrefix   string           `json:"emailPrefix,omitempty"`
}

type SetRequest struct {
	AccountID ID                 `json:"accountId"`
	IfInState *string            `json:"ifInState"`
	Create    map[ID]MaskedEmail `json:"create,omitempty"`
	Update    map[ID][]string    `json:"update,omitempty"`
	Destroy   []ID               `json:"destroy,omitempty"`
}

type SetError struct {
	Type        string  `json:"type"`
	Description *string `json:"description,omitempty"`
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
	IDs        []ID     `json:"ids"`
	Properties []string `json:"properties"`
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

func OpenSession(apiToken string) (*Client, error) {
	c := &Client{
		apiToken:   apiToken,
		httpClient: &http.Client{Timeout: timeout},
	}

	req, err := http.NewRequest(http.MethodGet, sessionURL, nil)
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

	if err := json.NewDecoder(resp.Body).Decode(&c.session); err != nil {
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

func newJMAPInvocation(methodName string, args any, callID string) (JMAPInvocation, error) {
	methodNameJSON, err := json.Marshal(methodName)
	if err != nil {
		return JMAPInvocation{}, fmt.Errorf("error marshalling method name: %w", err)
	}

	argsJSON, err := json.Marshal(args)
	if err != nil {
		return JMAPInvocation{}, fmt.Errorf("error marshalling args: %w", err)
	}

	callIDJSON, err := json.Marshal(callID)
	if err != nil {
		return JMAPInvocation{}, fmt.Errorf("error marshalling call ID: %w", err)
	}

	return JMAPInvocation{methodNameJSON, argsJSON, callIDJSON}, nil
}

func (c *Client) sendRequest(method string, args any) (json.RawMessage, error) {
	call, err := newJMAPInvocation(method, args, "a")
	if err != nil {
		return nil, fmt.Errorf("error building JMAP invocation: %w", err)
	}

	jmapRequest := JMAPRequest{
		Using:       []string{capCore, capMaskedEmail},
		MethodCalls: []JMAPInvocation{call},
	}
	body, err := json.Marshal(jmapRequest)
	if err != nil {
		return nil, fmt.Errorf("error marshalling JMAP request: %w", err)
	}

	req, err := http.NewRequest(http.MethodPost, c.session.APIURL, bytes.NewReader(body))
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

	var jmapResponse JMAPResponse
	if err := json.NewDecoder(resp.Body).Decode(&jmapResponse); err != nil {
		return nil, fmt.Errorf("error unmarshalling HTTP response: %w", err)
	}
	if len(jmapResponse.MethodResponses) == 0 {
		return nil, fmt.Errorf("no method responses in API reply")
	}

	return jmapResponse.MethodResponses[0][1], nil
}

func (c *Client) SendSetRequest(request SetRequest) (*SetResponse, error) {
	res, err := c.sendRequest("MaskedEmail/set", request)
	if err != nil {
		return nil, fmt.Errorf("error sending set request: %w", err)
	}

	var response SetResponse
	if err := json.Unmarshal(res, &response); err != nil {
		return nil, fmt.Errorf("error unmarshalling set response: %w", err)
	}
	return &response, nil
}

func (c *Client) SendGetRequest(request GetRequest) (*GetResponse, error) {
	res, err := c.sendRequest("MaskedEmail/get", request)
	if err != nil {
		return nil, fmt.Errorf("error sending get request: %w", err)
	}

	var response GetResponse
	if err := json.Unmarshal(res, &response); err != nil {
		return nil, fmt.Errorf("error unmarshalling get response: %w", err)
	}
	return &response, nil
}

func main() {
	if len(os.Args) < 2 {
		printUsageAndExit()
	}
	switch os.Args[1] {
	case "create":
		if err := createFlag.Parse(os.Args[2:]); err != nil {
			printUsageAndExit()
		}
		createAction()
	case "show":
		if err := showFlag.Parse(os.Args[2:]); err != nil {
			printUsageAndExit()
		}
		showAction()
	default:
		printUsageAndExit()
	}
}

func printUsageAndExit() {
	fmt.Fprintln(os.Stderr, usage)
	os.Exit(1)
}

func getAPIToken() (string, error) {
	currentUser, err := user.Current()
	if err != nil {
		return "", fmt.Errorf("error retrieving the current user: %w", err)
	}

	apiToken, err := keyring.Get(service, currentUser.Username)
	if err != nil {
		return "", fmt.Errorf("error obtaining API token from the keyring: %w", err)
	}
	return apiToken, nil
}

func setup() (*Client, ID, error) {
	apiToken, err := getAPIToken()
	if err != nil {
		return nil, "", fmt.Errorf("error getting API token: %w", err)
	}
	client, err := OpenSession(apiToken)
	if err != nil {
		return nil, "", fmt.Errorf("error opening session: %w", err)
	}

	accountID, ok := client.session.PrimaryAccounts[capMaskedEmail]
	if !ok {
		return nil, "", fmt.Errorf("no primary account with capability %s found", capMaskedEmail)
	}

	return client, accountID, nil
}

func createAction() {
	if *domain == "" {
		fmt.Fprintln(os.Stderr, "the domain cannot be empty")
		os.Exit(1)
	}
	if *description == "" {
		fmt.Fprintln(os.Stderr, "the description cannot be empty")
		os.Exit(1)
	}

	email, err := createEmail()
	if err != nil {
		fmt.Fprintf(os.Stderr, "%v\n", err)
		os.Exit(1)
	}
	fmt.Println(email.Email)
}

func createEmail() (*MaskedEmail, error) {
	client, accountID, err := setup()
	if err != nil {
		return nil, fmt.Errorf("error setting up client: %w", err)
	}

	request := SetRequest{
		AccountID: accountID,
		Create: map[ID]MaskedEmail{
			"new_masked_email": {
				State:       StatePending,
				ForDomain:   *domain,
				Description: *description,
			},
		},
	}

	response, err := client.SendSetRequest(request)
	if err != nil {
		return nil, fmt.Errorf("error sending set request: %w", err)
	}

	created, ok := response.Created["new_masked_email"]
	if !ok {
		return nil, fmt.Errorf("no email address created")
	}
	return &created, nil
}

func showAction() {
	if !(*state == "all" ||
		MaskedEmailState(*state) == StatePending ||
		MaskedEmailState(*state) == StateEnabled ||
		MaskedEmailState(*state) == StateDisabled ||
		MaskedEmailState(*state) == StateDeleted) {
		fmt.Fprintf(os.Stderr, "invalid state: %s\n", *state)
		os.Exit(1)
	}

	emails, err := showEmails()
	if err != nil {
		fmt.Fprintf(os.Stderr, "%v\n", err)
		os.Exit(1)
	}

	w := tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0)
	fmt.Fprintln(w, "Email\tState\tDomain\tDescription")
	for _, email := range emails {
		if *state == "all" || MaskedEmailState(*state) == email.State {
			fmt.Fprintf(w, "%s\t%s\t%s\t%s\n", email.Email, email.State, email.ForDomain, email.Description)
		}
	}
	w.Flush()
}

func showEmails() ([]MaskedEmail, error) {
	client, accountID, err := setup()
	if err != nil {
		return nil, fmt.Errorf("error setting up client: %w", err)
	}

	request := GetRequest{AccountID: accountID}

	response, err := client.SendGetRequest(request)
	if err != nil {
		return nil, fmt.Errorf("error sending get request: %w", err)
	}
	return response.MaskedEmails, nil
}
