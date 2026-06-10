import { z } from 'zod';

/**
 * web-app-zod's view of order-service's orders.get-order response.
 * The schema IS the usage declaration — wirefit runtime-imports this module
 * and converts it via the service's own z.toJSONSchema (Zod v4).
 *
 * z.uuid() maps to the uuid scalar — richer than the plain-TS path can see.
 * status picks a SUBSET of the provider's enum: adding a new enum value on
 * the provider side would break this consumer, and wirefit will say so.
 */
export const OrderViewSchema = z.object({
  order_id: z.uuid(),
  customer_email: z.string(),
  status: z.enum(['ACTIVE', 'BLOCKED', 'CLOSED']),
  items: z
    .array(
      z.object({
        sku: z.string(),
        qty: z.number(),
      }),
    )
    .optional(),
});
