interface Item {
  sku: string;
  qty: bigint;
}

export interface Nested {
  items: Item[];
  attributes: Record<string, string>;
}
