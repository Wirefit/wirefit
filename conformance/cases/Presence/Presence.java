package conformance;

import javax.annotation.Nonnull;
import java.util.Optional;

/** The three presence/nullability combinations expressible in both languages (SPEC §7). */
public class Presence {
    @Nonnull public String requiredNonNull;
    public String requiredNullable;             // plain reference: present, may be null
    public Optional<String> optionalNonNull;    // may be absent, never null
}
