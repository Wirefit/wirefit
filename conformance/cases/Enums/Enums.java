package conformance;

import javax.annotation.Nonnull;

public class Enums {
    @Nonnull public Status status;

    public enum Status { ACTIVE, BLOCKED, CLOSED }
}
