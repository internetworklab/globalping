import { CSSProperties } from "react";

export type ColorEncoding = {
  color: CSSProperties["color"];
  // [left, right)
  range: [number, number];
};

export function formatNumber(num: number): string {
  if (num === 0 || num === Infinity) {
    return num.toString();
  }
  return `${num}ms`;
}

export function latencyColorEncodingToString(encoding: ColorEncoding): string {
  return `[${formatNumber(encoding.range[0])}, ${formatNumber(
    encoding.range[1]
  )})`;
}

export const colorGreen = "#4caf50";
export const colorYellow = "#ff9800";
export const colorRed = "#f44336";
export const colorGrey = "#9e9e9e";

export const encodings: ColorEncoding[] = [
  { color: colorGreen, range: [0, 100] },
  { color: colorYellow, range: [100, 250] },
  { color: colorRed, range: [250, Infinity] },
];

export const getLatencyColor = (
  latency: number | null | undefined
): CSSProperties["color"] => {
  if (latency === null || latency === undefined) {
    return "inherit"; // Default color for missing data
  }

  for (const encoding of encodings) {
    if (latency >= encoding.range[0] && latency < encoding.range[1]) {
      return encoding.color;
    }
  }
  return colorGrey;
};
