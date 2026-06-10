"""Conformance fixtures: the same logical types as conformance/cases/*,
expressed in pydantic v2. `wirefit extractor-test` asserts hash-identity
with the Java/TS/Go corpus."""
from datetime import datetime
from typing import Annotated, Literal, Union

from pydantic import BaseModel, Field


class Scalars(BaseModel):
    name: str
    count: int          # → int64, pairs with Java long / TS bigint / Go int64
    price: float
    active: bool
    created: datetime


class Presence(BaseModel):
    requiredNonNull: str
    requiredNullable: str | None      # required, may be null
    optionalNonNull: str = ""         # may be absent, never null


class Enums(BaseModel):
    status: Literal["ACTIVE", "BLOCKED", "CLOSED"]


class Item(BaseModel):
    sku: str
    qty: int


class Nested(BaseModel):
    items: list[Item]
    attributes: dict[str, str]


class Recursion(BaseModel):
    name: str
    children: list["Recursion"]


class Card(BaseModel):
    method: Literal["card"]
    last4: str


class Iban(BaseModel):
    method: Literal["iban"]
    iban: str
    bic: str | None


Union_ = Annotated[Union[Card, Iban], Field(discriminator="method")]
