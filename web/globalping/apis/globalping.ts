
function getApiEndpoint(): string {
    return process.env.NEXT_PUBLIC_API_ENDPOINT || "https://globalping-api.netneighbor.me";
}

export async function getCurrentPingers(): Promise<string[]> {
  return fetch(`${getApiEndpoint()}/conns`)
    .then((res) => res.json())
    .then((data) => {
      const nodes: string[] = [];
      if (typeof data === "object") {
        for (const key in data) {
          const node = data[key];
          if (typeof node === "object") {
            if (
              node["attributes"] &&
              node["attributes"]["NodeName"] &&
              node["attributes"]["CapabilityPing"]
            ) {
              nodes.push(String(node["attributes"]["NodeName"]));
            }
          }
        }
      }
      return nodes;
    })
    .catch((err) => {
      console.error("Failed to get current pingers:", err);
      return [];
    });
}

export type PingSample = {
  from: string;
  target: string;

  // in the unit of milliseconds
  latencyMs: number;
};

export function generateFakePingSampleStream(
  sources: string[],
  targets: string[]
): ReadableStream<PingSample> {
  let intervalId: ReturnType<typeof setInterval> | null = null;

  return new ReadableStream<PingSample>({
    start(controller) {
      console.log("[dbg] start stream", sources, targets);

      intervalId = setInterval(() => {
        console.log("[dbg] interval invoked");

        // Generate all combinations of sources × targets
        for (const source of sources) {
          for (const target of targets) {
            // Generate a fake latency between 10ms and 300ms
            const latencyMs = Math.floor(Math.random() * 290) + 10;

            const sample: PingSample = {
              from: source,
              target: target,
              latencyMs: latencyMs,
            };

            console.log("[dbg] enqueueing sample:", sample);

            controller.enqueue(sample);
          }
        }
      }, 150); // Emit every 1 second
    },
    cancel() {
      // Clear the interval when the stream is cancelled
      if (intervalId !== null) {
        clearInterval(intervalId);
        intervalId = null;
      }
    },
  });
}

type PingEvent = {
    type?: "pkt_recv" | "ping_stats";
    data?: {
        target?: string;
        rtt?: number;
        min_rtt?: number;
        max_rtt?: number;
        avg_rtt?: number;
    }
    metadata?: {
        from?: string;
    }
}

/**
 example data:
{"type":"pkt_recv","data":{"dup":false,"id":24404,"ip_addr":"2.16.241.207","nbytes":32,"rtt":12,"seq":0,"target":"www.bing.com","ttl":54},"metadata":{"from":"vie1"}}
{"type":"pkt_recv","data":{"dup":false,"id":24404,"ip_addr":"2.16.241.207","nbytes":32,"rtt":12,"seq":1,"target":"www.bing.com","ttl":54},"metadata":{"from":"vie1"}}
{"type":"pkt_recv","data":{"dup":false,"id":24404,"ip_addr":"2.16.241.207","nbytes":32,"rtt":13,"seq":2,"target":"www.bing.com","ttl":54},"metadata":{"from":"vie1"}}
{"type":"pkt_recv","data":{"dup":false,"id":24404,"ip_addr":"2.16.241.207","nbytes":32,"rtt":15,"seq":3,"target":"www.bing.com","ttl":54},"metadata":{"from":"vie1"}}
{"type":"pkt_recv","data":{"dup":false,"id":24404,"ip_addr":"2.16.241.207","nbytes":32,"rtt":14,"seq":4,"target":"www.bing.com","ttl":54},"metadata":{"from":"vie1"}}
{"type":"ping_stats","data":{"avg_rtt":13,"ip_addr":"2.16.241.207","max_rtt":15,"min_rtt":12,"packet_loss":0,"packets_recv":5,"packets_recv_duplicates":0,"packets_sent":5,"rtts":[12,12,13,15,14],"std_dev_rtt":1,"target":"www.bing.com","ttls":[54,54,54,54,54]},"metadata":{"from":"vie1"}}
 */

function generatePingSampleStream(sources: string[], targets: string[]): ReadableStream<PingSample> {
    fetch(`${getApiEndpoint()}/ping?from=${sources.join(",")}&target=${targets.join(",")}`).then(res => {
        const reader = res.body?.getReader();
        reader?.read().then()
    })
    return new ReadableStream<PingSample>({
        start(controller) {
            console.log("[dbg] start stream", sources, targets);
        },
        cancel() {
            console.log("[dbg] cancel stream", sources, targets);
        },
    });
}
