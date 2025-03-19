# maskmail

[![Checked with mypy](https://www.mypy-lang.org/static/mypy_badge.svg)](https://mypy-lang.org/)
[![Linting: Ruff](https://img.shields.io/endpoint?url=https://raw.githubusercontent.com/astral-sh/ruff/main/assets/badge/v2.json)](https://github.com/astral-sh/ruff)

CLI access to FastMail's masked email API.

FastMail is capable of generating [masked 
emails](https://www.fastmail.com/features/masked-email/), which the user can 
provide when registering to online services, in order not to reveal their 
personal email address. However, generating one requires the user to login to 
the FastMail webapp, which takes a while. Luckily, FastMail provides a 
well-documented [JMAP API](https://www.fastmail.com/dev/), which can be 
utilized by third-party clients.

## Usage

First, install [uv](https://docs.astral.sh/uv/), as it is used to run the 
script as an executable file.

The `maskmail` client supports two basic operations: creating a new masked 
email, and listing the user's masked emails. These are the two most-common 
operations; to disable or delete an existing masked email, one should use the 
FastMail webapp.

To use the script, you need to create a FastMail API token with at least the 
masked email capabilities. This token can either be passed as the `--api-token
<TOKEN>` option, or as the `MASKMAIL_API_TOKEN` environment variable.

By default, the script sets a timeout of 5 seconds to each HTTP request. You 
can override this setting using the `--timeout <TIMEOUT>` option.

### Create a new masked email

To create a new masked email for the domain `https://example.com` with the 
description `Example description`, run the following:
```
$ MASKMAIL_API_TOKEN='<your-token>' ./maskmail.py create --domain 'https://example.com' --description 'Example description'
```
The script will output the newly-created email address to the standard output 
(exactly as it is received from the FastMail API).

The masked email is created in the `pending` state. If a message is received by 
this address, it will be automatically converted to the `enabled` state. If no 
message is received within 24 hours after creation, the address gets deleted.

### List existing masked emails

To list all masked emails of your account, run the following:
```
$ MASKMAIL_API_TOKEN='<your-token>' ./maskmail.py show
```
By default, the masked emails will be printed in an ASCII table with the 
columns `Email`, `State`, `Domain`, `Description` to the standard output.

You can provide the `--json` option, causing the script to print the addresses 
in JSON as a list of objects, each with keys corresponding to the ASCII table 
columns.

You can also provide the `--state <STATE>` option to only print the masked 
emails in the specified state.

## Development

The project uses the [uv](https://docs.astral.sh/uv/) package manager. However, 
as it is a standalone script, it uses [inline script 
metadata](https://packaging.python.org/en/latest/specifications/inline-script-metadata/#inline-script-metadata) 
to list them.

To lint the script, simply run the following:
```
$ uvx ruff check
$ uvx ruff format
$ uvx --with types-click,types-prettytable,types-requests,pydantic mypy .
```
