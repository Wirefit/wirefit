package conformance;

import jakarta.annotation.Nonnull;

public class EnumSingle {
    @Nonnull public Status status;

    public enum Status { ONLY }
}
