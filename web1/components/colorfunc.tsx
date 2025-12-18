export const getLatencyColor = (latency: number | null | undefined): string => {
  if (latency === null || latency === undefined) {
    return "inherit"; // Default color for missing data
  }
  if (latency <= 40) {
    return "#4caf50"; // Green for [0-40]ms
  } else if (latency <= 150) {
    return "#ff9800"; // Yellow for (40-150]ms
  } else {
    return "#f44336"; // Red for (150, +inf)ms
  }
};
