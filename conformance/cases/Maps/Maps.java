package conformance;

import jakarta.annotation.Nonnull;
import java.util.Map;

public class Maps {
    @Nonnull public Map<String, String> labels;
    @Nonnull public Map<String, Item> items;

    public static class Item {
        @Nonnull public String sku;
        public long qty;
    }
}
