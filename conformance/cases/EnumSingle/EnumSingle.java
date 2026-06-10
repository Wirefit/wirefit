package conformance;

import javax.annotation.Nonnull;

public class EnumSingle {
    @Nonnull public Status status;

    public enum Status { ONLY }
}
