import { ISO8601Timestamp } from "./globalping";

export type PingTaskType = "ping" | "traceroute" | "tcpping" | "dns";

export type DNSTransport = "udp" | "tcp";

export type DNSQueryType = "a" | "aaaa" | "cname" | "mx" | "ns" | "ptr" | "txt";

export type DNSTarget = {
  corrId: string;
  addrport: string;
  target: string;
  timeoutMs?: number;
  transport?: DNSTransport;
  queryType: DNSQueryType;
};

export type DNSResponse = {
  corrId?: string;
  server?: string;
  target?: string;
  query_type?: DNSQueryType;
  answers?: string[];
  answer_strings?: string[];
  error?: string;
  err_string?: string;
  io_timeout?: boolean;
  no_such_host?: boolean;
  // in the unit of nanoseconds
  elapsed?: number;
  started_at?: ISO8601Timestamp;
  // in the unit of nanoseconds
  timeout_specified?: number;
  transport_used?: DNSTransport;
};

export type DNSProbePlan = {
  transport: DNSTransport;
  type: DNSQueryType;
  domains: string[];
  resolvers: string[];
  domainsInput?: string;
  resolversInput?: string;
};

export function expandDNSProbePlan(plan: DNSProbePlan): {
  targets: DNSTarget[];
  targetsMap: Record<string, DNSTarget>;
} {
  const targets: DNSTarget[] = [];
  const targetsMap: Record<string, DNSTarget> = {};
  for (const domain of plan.domains) {
    for (const resolver of plan.resolvers) {
      const target: DNSTarget = {
        corrId: targets.length.toString(),
        addrport: resolver,
        target: domain,
        transport: plan.transport,
        queryType: plan.type,
      };
      targets.push(target);
      targetsMap[target.corrId] = target;
    }
  }

  targets.sort((a, b) => {
    const x = parseInt(a.corrId || "0");
    const y = parseInt(b.corrId || "0");
    return x - y;
  });

  return { targets, targetsMap };
}

export type PendingTask = {
  sources: string[];
  targets: string[];
  taskId: number;
  type: PingTaskType;
  preferV4?: boolean;
  preferV6?: boolean;
  useUDP?: boolean;
  pmtu?: boolean;
  dnsProbePlan?: DNSProbePlan;
};

export type ExactLocation = {
  Longitude: number;
  Latitude: number;
};
