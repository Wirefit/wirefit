/**
 * web-app-ts's view of order-service's orders.get-order response.
 *
 * Note `total_cents: number` against the provider's Java `long`: wirefit
 * flags this pairing as lossy (int64 → float64, unsafe beyond 2^53) — the
 * cross-language check no schema-registry or OpenAPI diff provides (SPEC F7).
 */
export interface OrderView {
  order_id: string;
  customer_email: string;
  total_cents?: number;
}
