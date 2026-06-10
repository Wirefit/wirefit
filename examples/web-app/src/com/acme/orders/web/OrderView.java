package com.acme.orders.web;

import com.fasterxml.jackson.annotation.JsonProperty;

import jakarta.annotation.Nonnull;
import java.util.UUID;

/**
 * web-app's view of order-service's orders.get-order response.
 * Reading only what it needs — this projection IS its registered usage.
 */
public class OrderView {

    @Nonnull
    @JsonProperty("order_id")
    public UUID orderId;

    @Nonnull
    @JsonProperty("customer_email")
    public String customerEmail;
}
