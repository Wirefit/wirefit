package conformance;

import javax.annotation.Nonnull;
import java.util.List;
import java.util.Map;

public class Nested {
    @Nonnull public List<Item> items;
    @Nonnull public Map<String, String> attributes;

    public static class Item {
        @Nonnull public String sku;
        public long qty;
    }
}
