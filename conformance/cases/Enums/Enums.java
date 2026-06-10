package conformance;

import jakarta.annotation.Nonnull;

public class Enums {
    @Nonnull public Status status;

    public enum Status { ACTIVE, BLOCKED, CLOSED }
}
