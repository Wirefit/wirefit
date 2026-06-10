package com.acme.inventory;

import com.fasterxml.jackson.databind.ObjectMapper;
import com.fasterxml.jackson.databind.PropertyNamingStrategies;

import javax.annotation.Nonnull;
import java.time.Instant;

public class StockLevelResponse {

    @Nonnull public String sku;
    public int available;
    public int reserved;
    @Nonnull public Instant asOf;

    /**
     * ObjectMapper provider referenced by contracts.yaml settings.java-mapper —
     * exercises the wirefit.mapper hint path (stands in for Spring config).
     */
    public static ObjectMapper wirefitMapper() {
        return new ObjectMapper()
                .setPropertyNamingStrategy(PropertyNamingStrategies.SNAKE_CASE);
    }
}
