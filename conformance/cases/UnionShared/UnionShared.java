package conformance;

import com.fasterxml.jackson.annotation.JsonSubTypes;
import com.fasterxml.jackson.annotation.JsonTypeInfo;

import javax.annotation.Nonnull;

@JsonTypeInfo(use = JsonTypeInfo.Id.NAME, include = JsonTypeInfo.As.PROPERTY, property = "kind")
@JsonSubTypes({
        @JsonSubTypes.Type(value = UnionShared.Card.class, name = "card"),
        @JsonSubTypes.Type(value = UnionShared.Iban.class, name = "iban"),
})
public abstract class UnionShared {

    @Nonnull public String ref;   // shared field, inherited into every branch

    public static class Card extends UnionShared {
        @Nonnull public String last4;
    }

    public static class Iban extends UnionShared {
        @Nonnull public String iban;
    }
}
