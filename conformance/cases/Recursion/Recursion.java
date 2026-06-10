package conformance;

import jakarta.annotation.Nonnull;
import java.util.List;

public class Recursion {
    @Nonnull public String name;
    @Nonnull public List<Recursion> children;
}
