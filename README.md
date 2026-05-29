# maskmail

CLI access to FastMail's masked email API.

FastMail is capable of generating [masked emails](https://www.fastmail.com/features/masked-email/), which the user can provide when registering to online services, in order not to reveal their personal email address. However, generating one requires the user to login to the FastMail webapp, which takes a while. Luckily, FastMail provides a well-documented [JMAP API](https://www.fastmail.com/dev/), which can be utilized by third-party clients.

## Usage

Build and run the binary

```
$ go build
$ ./maskmail ACTION [OPTIONS]
```

The `maskmail` client supports two basic operations: creating a new masked email, and listing the user's masked emails. These are the two most-common operations; to disable or delete an existing masked email, one should use the FastMail webapp.

To use the program, you need to create a FastMail API token with at least the masked email capabilities and store it in the system key chain. The program uses the [go-keyring](https://github.com/zalando/go-keyring) package to access it. Store the token by following the instructions at [go-keyring#direct-cli-usage](https://github.com/zalando/go-keyring#direct-cli-usage), with the service name being `maskmail` and username being your login username.

The program sets a timeout of 5 seconds to each HTTP request.

### Create a new masked email

To create a new masked email for the domain `https://example.com` with the description `Example description`, run the following:

```
$ ./maskmail create -domain 'https://example.com' -description 'Example description'
```

The program will output the newly-created email address to the standard output (exactly as it is received from the FastMail API).

The masked email is created in the `pending` state. If a message is received by this address, it will be automatically converted to the `enabled` state. If no message is received within 24 hours after creation, the address gets deleted.

### List existing masked emails

To list all masked emails of your account, run the following:

```
$ ./maskmail show
```

By default, the masked emails will be printed in a table with the columns `Email`, `State`, `Domain`, `Description` to the standard output.

You can also provide the `-state <STATE>` option to only print the masked emails in the specified state.
