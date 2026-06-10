package com.acme.orders;

import com.fasterxml.jackson.annotation.JsonIgnore;
import com.fasterxml.jackson.annotation.JsonInclude;
import com.fasterxml.jackson.annotation.JsonProperty;
import com.fasterxml.jackson.databind.PropertyNamingStrategies;
import com.fasterxml.jackson.databind.annotation.JsonNaming;

import jakarta.annotation.Nonnull;
import java.math.BigDecimal;
import java.util.List;
import java.util.Map;
import java.util.Optional;
import java.util.UUID;

@JsonNaming(PropertyNamingStrategies.SnakeCaseStrategy.class)
public class OrderResponse {

    @Nonnull
    public UUID orderId;                  // -> order_id: uuid, required, non-nullable

    public long totalCents;               // -> total_cents: int64, required, non-nullable (primitive)

    @Nonnull public String customerEmail; // -> customer_email: string, required, non-nullable

    @JsonInclude(JsonInclude.Include.NON_NULL)
    public String couponCode;             // -> coupon_code: optional, non-nullable (null suppressed)

    public Optional<String> note;         // -> note: optional, non-nullable

    @Nonnull
    @JsonProperty(required = true)
    public Status status;                 // -> status: enum, required (explicit), nullable

    @Nonnull public List<LineItem> items;          // -> items: array of object

    public Map<String, String> attributes; // -> attributes: open object

    @JsonIgnore
    public String internalTraceId;        // dropped

    public enum Status { ACTIVE, BLOCKED, CLOSED }

    public static class LineItem {
        @Nonnull public String sku;
        public int qty;
        @Nonnull public BigDecimal unitPrice;  // -> unit_price: decimal
    }
}
