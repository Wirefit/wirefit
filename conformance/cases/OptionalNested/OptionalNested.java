package conformance;

import jakarta.annotation.Nonnull;
import java.util.Optional;

public class OptionalNested {
    public Optional<Inner> inner = Optional.empty();

    public static class Inner {
        @Nonnull public String name;
    }
}
