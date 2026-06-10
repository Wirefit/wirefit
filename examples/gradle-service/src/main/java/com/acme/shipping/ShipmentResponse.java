package com.acme.shipping;

import javax.annotation.Nonnull;
import java.time.Instant;
import java.util.List;

public class ShipmentResponse {

    @Nonnull public String trackingId;
    @Nonnull public Status status;
    @Nonnull public List<Leg> legs;
    @Nonnull public Instant eta;

    public enum Status { PENDING, IN_TRANSIT, DELIVERED }

    public static class Leg {
        @Nonnull public String carrier;
        public long distanceMeters;
    }
}
