export type PingTaskType = "ping" | "traceroute" | "tcpping" | "dns";

export type PendingTask = {
  sources: string[];
  targets: string[];
  taskId: number;
  type: PingTaskType;
  preferV4?: boolean;
  preferV6?: boolean;
  useUDP?: boolean;
  pmtu?: boolean;
};

export type ExactLocation = {
  Longitude: number;
  Latitude: number;
};
