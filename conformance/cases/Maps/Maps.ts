interface Item {
  sku: string;
  qty: bigint;
}

export interface Maps {
  labels: Record<string, string>;
  items: Record<string, Item>;
}
