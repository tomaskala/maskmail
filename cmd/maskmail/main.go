package main

import (
	"context"
	"flag"
	"fmt"
	"net/http"
	"os"
	"os/user"
	"text/tabwriter"
	"time"

	"github.com/zalando/go-keyring"
	"tomaskala.com/maskmail"
)

const (
	service = "maskmail"
	usage   = `maskmail <create|show> [OPTIONS]

OPTIONS for create
      -domain       Domain this masked email is for (required)
      -description  Description of the masked email's usage (required)

OPTIONS for show
      -state  Only show masked emails in this state (all/pending/enabled/disabled/deleted)`

	timeout = 10 * time.Second
)

var (
	createFlag  = flag.NewFlagSet("create", flag.ContinueOnError)
	domain      = createFlag.String("domain", "", "Domain this masked email is for")
	description = createFlag.String("description", "", "Description of the masked email's usage")

	showFlag = flag.NewFlagSet("show", flag.ContinueOnError)
	state    = showFlag.String("state", "all", "Only show masked emails in this state (all/pending/enabled/disabled/deleted)")
)

func main() {
	if len(os.Args) < 2 {
		printUsageAndExit()
	}

	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()

	switch os.Args[1] {
	case "create":
		if err := createFlag.Parse(os.Args[2:]); err != nil {
			printUsageAndExit()
		}

		createAction(ctx)
	case "show":
		if err := showFlag.Parse(os.Args[2:]); err != nil {
			printUsageAndExit()
		}

		showAction(ctx)
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

func setup(ctx context.Context) (*maskmail.Client, maskmail.ID, error) {
	apiToken, err := getAPIToken()
	if err != nil {
		return nil, "", fmt.Errorf("error getting API token: %w", err)
	}

	httpClient := &http.Client{}
	client, err := maskmail.OpenSession(ctx, apiToken, httpClient)
	if err != nil {
		return nil, "", fmt.Errorf("error opening session: %w", err)
	}

	accountID, ok := client.AccountID()
	if !ok {
		return nil, "", fmt.Errorf("no primary account with masked email capability found")
	}

	return client, accountID, nil
}

func createAction(ctx context.Context) {
	if *domain == "" {
		fmt.Fprintln(os.Stderr, "the domain cannot be empty")
		os.Exit(1)
	}

	if *description == "" {
		fmt.Fprintln(os.Stderr, "the description cannot be empty")
		os.Exit(1)
	}

	email, err := createEmail(ctx)
	if err != nil {
		fmt.Fprintf(os.Stderr, "%v\n", err)
		os.Exit(1)
	}

	fmt.Println(email.Email)
}

func createEmail(ctx context.Context) (*maskmail.MaskedEmail, error) {
	client, accountID, err := setup(ctx)
	if err != nil {
		return nil, fmt.Errorf("error setting up client: %w", err)
	}

	request := maskmail.SetRequest{
		AccountID: accountID,
		Create: map[maskmail.ID]maskmail.MaskedEmail{
			"new_masked_email": {
				State:       maskmail.StatePending,
				ForDomain:   *domain,
				Description: *description,
			},
		},
	}

	response, err := client.SendSetRequest(ctx, request)
	if err != nil {
		return nil, fmt.Errorf("error sending set request: %w", err)
	}

	created, ok := response.Created["new_masked_email"]
	if !ok {
		return nil, fmt.Errorf("no email address created")
	}

	return &created, nil
}

func showAction(ctx context.Context) {
	if !(*state == "all" ||
		maskmail.MaskedEmailState(*state) == maskmail.StatePending ||
		maskmail.MaskedEmailState(*state) == maskmail.StateEnabled ||
		maskmail.MaskedEmailState(*state) == maskmail.StateDisabled ||
		maskmail.MaskedEmailState(*state) == maskmail.StateDeleted) {
		fmt.Fprintf(os.Stderr, "invalid state: %s\n", *state)
		os.Exit(1)
	}

	emails, err := showEmails(ctx)
	if err != nil {
		fmt.Fprintf(os.Stderr, "%v\n", err)
		os.Exit(1)
	}

	w := tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0)
	fmt.Fprintln(w, "Email\tState\tDomain\tDescription")
	for _, email := range emails {
		if *state == "all" || maskmail.MaskedEmailState(*state) == email.State {
			fmt.Fprintf(w, "%s\t%s\t%s\t%s\n", email.Email, email.State, email.ForDomain, email.Description)
		}
	}
	w.Flush()
}

func showEmails(ctx context.Context) ([]maskmail.MaskedEmail, error) {
	client, accountID, err := setup(ctx)
	if err != nil {
		return nil, fmt.Errorf("error setting up client: %w", err)
	}

	request := maskmail.GetRequest{AccountID: accountID}

	response, err := client.SendGetRequest(ctx, request)
	if err != nil {
		return nil, fmt.Errorf("error sending get request: %w", err)
	}

	return response.MaskedEmails, nil
}
