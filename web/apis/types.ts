export type PingTaskType = "ping" | "traceroute";

export type PendingTask = {
  sources: string[];
  targets: string[];
  taskId: number;
  type: PingTaskType;
};

export type ExactLocation = {
  Longitude: number;
  Latitude: number;
};
