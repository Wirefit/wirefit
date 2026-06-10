package com.acme.orders;

import com.fasterxml.jackson.annotation.JsonSubTypes;
import com.fasterxml.jackson.annotation.JsonTypeInfo;

import javax.annotation.Nonnull;
import java.time.Instant;

@JsonTypeInfo(use = JsonTypeInfo.Id.NAME, include = JsonTypeInfo.As.PROPERTY, property = "method")
@JsonSubTypes({
        @JsonSubTypes.Type(value = Payment.Card.class, name = "card"),
        @JsonSubTypes.Type(value = Payment.Iban.class, name = "iban"),
})
public abstract class Payment {

    @Nonnull
    public Instant createdAt;   // inherited into every branch

    public static class Card extends Payment {
        @Nonnull public String last4;
    }

    public static class Iban extends Payment {
        @Nonnull public String iban;
        public String bic;      // nullable reference
    }
}
