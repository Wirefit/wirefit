package conformance;

import javax.annotation.Nonnull;
import java.time.Instant;

public class Scalars {
    @Nonnull public String name;
    public long count;        // int64 — pairs with TS bigint
    public double price;      // float64 — pairs with TS number
    public boolean active;
    @Nonnull public Instant created;
}
