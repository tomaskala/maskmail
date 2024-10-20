import re
import sys
from datetime import datetime
from enum import StrEnum
from typing import Annotated, Any, Generic, TypeVar

import click
import requests
from click import Context
from prettytable import PrettyTable
from pydantic import BaseModel, ConfigDict, Field, StringConstraints, ValidationError
from requests import Timeout

SESSION_URL = "https://api.fastmail.com/jmap/session"
CAP_CORE = "urn:ietf:params:jmap:core"
CAP_MASKED_EMAIL = "https://www.fastmail.com/dev/maskedemail"


Id = Annotated[
    str,
    StringConstraints(
        min_length=1, max_length=255, pattern=re.compile(r"[A-Za-z0-9\-_]+")
    ),
]


class Account(BaseModel):
    name: str
    is_personal: bool = Field(alias="isPersonal")
    is_read_only: bool = Field(alias="isReadOnly")
    account_capabilities: dict[str, Any] = Field(alias="accountCapabilities")


class Session(BaseModel):
    capabilities: dict[str, Any]
    accounts: dict[Id, Account]
    primary_accounts: dict[str, Id] = Field(alias="primaryAccounts")
    username: str
    api_url: str = Field(alias="apiUrl")
    download_url: str = Field(alias="downloadUrl")
    upload_url: str = Field(alias="uploadUrl")
    event_source_url: str = Field(alias="eventSourceUrl")
    state: str


class MaskedEmailState(StrEnum):
    PENDING = "pending"
    ENABLED = "enabled"
    DISABLED = "disabled"
    DELETED = "deleted"


class MaskedEmail(BaseModel):
    model_config = ConfigDict(populate_by_name=True)

    masked_email_id: str = Field(alias="id")
    email: str
    state: MaskedEmailState | None = None
    for_domain: str | None = Field(alias="forDomain", default=None)
    description: str | None = None
    last_message_at: datetime | None = Field(alias="lastMessageAt")
    created_at: datetime = Field(alias="createdAt")
    created_by: str = Field(alias="createdBy")
    url: str | None
    email_prefix: str | None = Field(alias="emailPrefix", default=None)


class GetRequest(BaseModel):
    model_config = ConfigDict(populate_by_name=True)

    account_id: Id = Field(alias="accountId")
    ids: list[Id] | None
    properties: list[str] | None


class GetResponse(BaseModel):
    account_id: Id = Field(alias="accountId")
    state: str
    masked_emails: list[MaskedEmail] = Field(alias="list")
    not_found: list[Id] = Field(alias="notFound")


class SetRequest(BaseModel):
    model_config = ConfigDict(populate_by_name=True)

    account_id: Id = Field(alias="accountId")
    if_in_state: str | None = Field(alias="ifInState")
    create: dict[Id, MaskedEmail] | None
    update: dict[Id, list[str]] | None
    destroy: list[Id] | None


class SetError(BaseModel):
    error_type: str = Field(alias="type")
    description: str | None


class SetResponse(BaseModel):
    account_id: Id = Field(alias="accountId")
    old_state: str | None = Field(alias="oldState")
    new_state: str | None = Field(alias="newState")
    created: dict[Id, MaskedEmail] | None
    updated: dict[Id, MaskedEmail | None] | None
    destroyed: list[Id] | None
    not_created: dict[Id, SetError] | None = Field(alias="notCreated", default=None)
    not_updated: dict[Id, SetError] | None = Field(alias="notUpdated", default=None)
    not_destroyed: dict[Id, SetError] | None = Field(alias="notDestroyed", default=None)


S = TypeVar("S", GetRequest, SetRequest)
T = TypeVar("T", GetResponse, SetResponse)


class Request(BaseModel, Generic[S]):
    model_config = ConfigDict(populate_by_name=True)

    using: list[str]
    method_calls: list[tuple[str, S, str]] = Field(alias="methodCalls")


class Response(BaseModel, Generic[T]):
    session_state: str = Field(alias="sessionState")
    method_responses: list[tuple[str, T, str]] = Field(alias="methodResponses")


def make_headers(api_token: str) -> dict[str, str]:
    return {
        "Authorization": f"Bearer {api_token}",
        "Content-Type": "application/json; charset=utf-8",
    }


def get_session(api_token: str, timeout: int) -> Session:
    response = requests.get(
        SESSION_URL, headers=make_headers(api_token), timeout=timeout
    )
    response.raise_for_status()
    return Session(**response.json())


def send_get_request(
    url: str, api_token: str, request: GetRequest, timeout: int
) -> GetResponse:
    payload = Request[GetRequest](
        using=[CAP_CORE, CAP_MASKED_EMAIL],
        method_calls=[("MaskedEmail/get", request, "a")],
    )
    response = requests.post(
        url,
        headers=make_headers(api_token),
        json=payload.model_dump(by_alias=True),
        timeout=timeout,
    )
    response.raise_for_status()
    return Response[GetResponse](**response.json()).method_responses[0][1]


def send_set_request(
    url: str, api_token: str, request: SetRequest, timeout: int
) -> SetResponse:
    payload = Request[SetRequest](
        using=[CAP_CORE, CAP_MASKED_EMAIL],
        method_calls=[("MaskedEmail/set", request, "a")],
    )
    response = requests.post(
        url,
        headers=make_headers(api_token),
        json=payload.model_dump(by_alias=True),
        timeout=timeout,
    )
    response.raise_for_status()
    return Response[SetResponse](**response.json()).method_responses[0][1]


@click.group()
@click.option(
    "--api-token",
    type=str,
    envvar="MASKMAIL_API_TOKEN",
    required=True,
    help="{} {}".format(
        "FastMail API token with Masked Email capabilities.",
        "The MASKMAIL_API_TOKEN environment variable should be preferred.",
    ),
)
@click.option(
    "--timeout",
    type=int,
    default=5,
    help="Timeout for the API calls in seconds",
)
@click.pass_context
def cli(ctx: Context, api_token: str, timeout: int) -> None:
    ctx.ensure_object(dict)
    ctx.obj["api_token"] = api_token
    ctx.obj["timeout"] = timeout


@cli.command()
@click.option(
    "--domain",
    type=str,
    required=True,
    help="Domain this masked email is for, in the format 'https://www.example.com'",
)
@click.option(
    "--description",
    type=str,
    required=True,
    help="Description of the masked email's usage",
)
@click.pass_context
def create(ctx: Context, domain: str, description: str) -> None:
    api_token = ctx.obj["api_token"]
    timeout = ctx.obj["timeout"]

    try:
        session = get_session(api_token, timeout)
    except Timeout:
        click.echo("Timed out when querying the session", err=True)
        sys.exit(1)
    except ValidationError:
        click.echo("Error validating the session response", err=True)
        sys.exit(1)

    request = SetRequest(
        account_id=session.primary_accounts[CAP_MASKED_EMAIL],
        if_in_state=None,
        create={
            "new_masked_email": MaskedEmail.model_construct(  # type: ignore[call-arg]
                state=MaskedEmailState.PENDING,
                for_domain=domain,
                description=description,
            ),
        },
        update=None,
        destroy=None,
    )

    try:
        response = send_set_request(session.api_url, api_token, request, timeout)
    except Timeout:
        click.echo("Timed out when attempting to create the masked email", err=True)
        sys.exit(1)
    except ValidationError:
        click.echo("Error validating the masked email creation response", err=True)
        sys.exit(1)

    if response.created:
        click.echo(response.created["new_masked_email"].email)
    else:
        click.echo("Error: No address was created", err=True)


@cli.command()
@click.option(
    "--state",
    type=click.Choice(MaskedEmailState),  # type: ignore[arg-type]
    help="Only list masked emails in this state",
)
@click.option(
    "--json",
    type=bool,
    is_flag=True,
    default=False,
    help="Output to JSON instead of a table",
)
@click.pass_context
def show(ctx: Context, state: MaskedEmailState | None, json: bool) -> None:
    api_token = ctx.obj["api_token"]
    timeout = ctx.obj["timeout"]

    try:
        session = get_session(api_token, timeout)
    except Timeout:
        click.echo("Timed out when querying the session", err=True)
        sys.exit(1)
    except ValidationError:
        click.echo("Error validating the session response", err=True)
        sys.exit(1)

    request = GetRequest(
        account_id=session.primary_accounts[CAP_MASKED_EMAIL],
        ids=None,
        properties=None,
    )

    try:
        response = send_get_request(session.api_url, api_token, request, timeout)
    except Timeout:
        click.echo("Timed out when attempting to list masked emails", err=True)
        sys.exit(1)
    except ValidationError:
        click.echo("Error validating the masked email listing response", err=True)
        sys.exit(1)

    table = PrettyTable(
        align="r", field_names=["Email", "State", "Domain", "Description"]
    )

    for masked_email in response.masked_emails:
        if state is None or masked_email.state == state:
            table.add_row(
                [
                    masked_email.email,
                    masked_email.state,
                    masked_email.for_domain,
                    masked_email.description,
                ]
            )

    if json:
        click.echo(table.get_json_string(header=False))
    else:
        click.echo(table)


if __name__ == "__main__":
    cli()
