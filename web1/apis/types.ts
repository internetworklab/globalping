export type PingTaskType = "ping" | "traceroute";

export type PendingTask = {
  sources: string[];
  targets: string[];
  taskId: string;
  type: PingTaskType;
};

export type ExactLocation = {
  Longitude: number;
  Latitude: number;
}
