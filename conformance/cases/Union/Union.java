package conformance;

import com.fasterxml.jackson.annotation.JsonSubTypes;
import com.fasterxml.jackson.annotation.JsonTypeInfo;

import javax.annotation.Nonnull;

@JsonTypeInfo(use = JsonTypeInfo.Id.NAME, include = JsonTypeInfo.As.PROPERTY, property = "method")
@JsonSubTypes({
        @JsonSubTypes.Type(value = Union.Card.class, name = "card"),
        @JsonSubTypes.Type(value = Union.Iban.class, name = "iban"),
})
public abstract class Union {

    public static class Card extends Union {
        @Nonnull public String last4;
    }

    public static class Iban extends Union {
        @Nonnull public String iban;
        public String bic;   // present, may be null
    }
}
