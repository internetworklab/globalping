import { ExactLocation } from "./types";

function getApiEndpoint(): string {
  return (
    process.env.NEXT_PUBLIC_API_ENDPOINT ||
    "https://globalping-api.netneighbor.me"
  );
}

function sortAndDedup(nodes: string[]): string[] {
  const nodeSet = new Set<string>(nodes);
  const sorted = Array.from(nodeSet);
  sorted.sort();
  return sorted;
}

export async function getCurrentPingers(
  extraLabels?: Record<string, string>
): Promise<string[]> {
  return fetch(`${getApiEndpoint()}/conns`)
    .then((res) => res.json())
    .then((data) => {
      const nodes: string[] = [];
      if (typeof data === "object") {
        for (const key in data) {
          const node = data[key];
          if (typeof node === "object") {
            if (!node["attributes"] || typeof node["attributes"] !== "object") {
              continue;
            }
            if (extraLabels) {
              let allMatch = true;
              for (const key in extraLabels) {
                if (
                  !node["attributes"][key] ||
                  node["attributes"][key] !== extraLabels[key]
                ) {
                  allMatch = false;
                  break;
                }
              }
              if (!allMatch) {
                continue;
              }
            }
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

      return sortAndDedup(nodes);
    })
    .catch((err) => {
      console.error("Failed to get current pingers:", err);
      return [];
    });
}

// A stream source is said to adaptable if it is convertible to a PingSample stream.
export type PingSample = {
  isTimeout: boolean;

  // the node name where the icmp echo request is originated from, it's mostly just a label, not something that is pingable
  from: string;

  // the destination of the icmp echo request, in dns un-resolved form
  target: string;

  // in the unit of milliseconds
  latencyMs?: number;

  // for receiving packets, the mss option value as announced by the peer
  mss?: number;

  // the ttl of the sent packet, ttl must be present, even in the case of timeout
  ttl: number;

  // the ttl of the receiving packet
  receivedTTL?: number;

  // the size of the receiving packet
  receivedSize?: number;

  // the seq of the sent packet, seq must be present, even in the case of timeout
  seq: number;

  // the address of the peer, could be the original destination, or some middlebox halfway
  peer?: string;

  // the rdns of the peer, not necessarily available
  peerRdns?: string;

  peerASN?: string;

  peerLocation?: string;

  peerISP?: string;

  peerExactLocation?: ExactLocation;

  lastHop?: boolean;

  pmtu?: number;
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

        let seq = 0;
        // Generate all combinations of sources × targets
        for (const source of sources) {
          for (const target of targets) {
            // Generate a fake latency between 10ms and 300ms
            const latencyMs = Math.floor(Math.random() * 290) + 10;

            const sample: PingSample = {
              isTimeout: false,
              from: source,
              target: target,
              latencyMs: latencyMs,
              ttl: 64,
              seq: seq,
            };
            seq++;

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

export type ISO8601Timestamp = string;

type RawPingEventICMPReply = {
  ICMPTypeV4?: number;
  ICMPTypeV6?: number;
  ID?: number;
  Peer?: string;
  PeerRDNS?: string[];
  PeerASN?: string;
  PeerLocation?: string;
  PeerISP?: string;
  PeerExactLocation?: ExactLocation;

  ReceivedAt?: ISO8601Timestamp;

  // Seq of the reply packet
  Seq?: number;
  // size of icmp, without the ip(v4/v6) header
  Size?: number;
  // TTl of the reply packet
  TTL?: number;

  LastHop?: boolean;

  SetMTUTo?: number;
};

type RawTCPPingEventDataDetails = {
  Seq?: number;
  SrcIP?: string;
  SrcPort?: number;
  Request?: {
    DstIP?: string;
    DstPort?: number;
    Timeout?: number; // in unit of nanoseconds
    TTL?: number; // likely be null, undefined, becuase in reality we use default value
    Seq?: number; // most likely be some random uint32, it's what we use to send over the wire, in tcp header
    Ack?: number; // same, in tcp header
    Window?: number; // in tcp header as well
  };
  SentAt?: ISO8601Timestamp;
  ReceivedAt?: ISO8601Timestamp;
  ReceivedPkt?: {
    SrcIP?: string;
    DstIP?: string;
    Payload?: string; // some base64
    TCP?: {
      Contents?: string; // some base64
      Payload?: string; // some base64
      SrcPort?: number;
      DstPort?: number;
      Seq?: number;
      Ack?: number;
      DataOffset?: number;
      FIN?: boolean;
      SYN?: boolean;
      RST?: boolean;
      PSH?: boolean;
      ACK?: boolean;
      URG?: boolean;
      ECE?: boolean;
      CWR?: boolean;
      NS?: boolean;
      Window?: number;
      Checksum?: number;
      Urgent?: number;
      Options?: {
        OptionType?: number;
        OptionLength?: number;
        OptionData?: string; // some base64
      }[];
      Padding?: null;
    };

    // for receiving packets, this would be the ttl(or hoplimit) found in the ip header
    TTL?: number;

    // defined as IP packet total len, i.e., size of ip header + size of ip payload
    Size?: number;

    // for receiving packets, this would be the size of the tcp header
    TCPHeaderLen?: number;

    // for receiving packets, this would be the mss option value announced by the sender
    MSS?: number;

    // for receiving packets, this would be the rdns of the peer (that is, the SrcIP)
    PeerRDNS?: string[];
    PeerASN?: string;
    PeerISP?: string;
    PeerLocation?: string;
    PeerExactLocation?: ExactLocation;
  };
  RTT?: number; // in unit of nanoseconds
  SentTTL?: number; // ttl or hoplimit in the ip header that was sent
};

type RawTCPPingEventData = {
  Type: "received" | "timeout";
  Details: RawTCPPingEventDataDetails;
};

type RawPingEventData = {
  RTTMilliSecs?: number[];
  RTTNanoSecs?: number[];
  Raw?: RawPingEventICMPReply[];
  ReceivedAt?: ISO8601Timestamp[];
  SentAt?: ISO8601Timestamp;

  // Seq of the sent packet
  Seq?: number;

  // TTL of the sent packet
  TTL?: number;
};

type RawPingEventMetadata = {
  from?: string;
  target?: string;
};

type RawTCPPingEvent = {
  data?: RawTCPPingEventData;
  metadata?: RawPingEventMetadata;
};

// Raw event returned by the API
type RawPingEvent = {
  data?: RawPingEventData;
  metadata?: RawPingEventMetadata;
};

type TokenObject = {
  content: string;
};

// Generated by: deepseek
class LineTokenizer extends TransformStream<string, TokenObject> {
  constructor() {
    let buffer = "";

    super({
      transform(
        chunk: string,
        controller: TransformStreamDefaultController<TokenObject>
      ) {
        buffer += chunk;

        let newlineIndex;
        while ((newlineIndex = buffer.indexOf("\n")) !== -1) {
          const line = buffer.slice(0, newlineIndex);

          const cleanLine = line.endsWith("\r") ? line.slice(0, -1) : line;

          controller.enqueue({ content: cleanLine });

          buffer = buffer.slice(newlineIndex + 1);
        }
      },

      flush(controller: TransformStreamDefaultController<TokenObject>) {
        if (buffer) {
          const cleanLine = buffer.endsWith("\r")
            ? buffer.slice(0, -1)
            : buffer;
          controller.enqueue({ content: cleanLine });
        }
      },
    });
  }
}

class JSONLineDecoder extends TransformStream<TokenObject, unknown> {
  constructor() {
    super({
      transform(
        chunk: TokenObject,
        controller: TransformStreamDefaultController<unknown>
      ) {
        try {
          const unknownObj = JSON.parse(chunk.content);
          controller.enqueue(unknownObj);
        } catch (err) {
          console.error("Failed to parse JSON line:", err);
        }
      },
    });
  }
}

class TCPPingEventAdapter extends TransformStream<RawTCPPingEvent, PingSample> {
  constructor() {
    super({
      transform(
        chunk: RawTCPPingEvent,
        controller: TransformStreamDefaultController<PingSample>
      ) {
        const maybeSample = pingSampleFromTCPEvent(chunk);
        if (maybeSample) {
          controller.enqueue(maybeSample);
        }
      },
    });
  }
}

class PingEventAdapter extends TransformStream<RawPingEvent, PingSample> {
  constructor() {
    super({
      transform(
        chunk: RawPingEvent,
        controller: TransformStreamDefaultController<PingSample>
      ) {
        const maybeSample = pingSampleFromEvent(chunk);
        if (maybeSample) {
          controller.enqueue(maybeSample);
        }
      },
    });
  }
}

function pingSampleFromTCPEvent(
  event: RawTCPPingEvent
): PingSample | undefined {
  const from = event.metadata?.from || "";
  const target = event.metadata?.target || "";
  if (!event.data) {
    return undefined;
  }

  const details = event.data?.Details;
  if (!details) {
    return undefined;
  }
  if (event?.data?.Type !== "received") {
    return {
      isTimeout: true,
      from: from,
      target: target,
      ttl: event?.data?.Details?.SentTTL ?? 0,
      seq: event?.data?.Details?.Seq ?? 0,
    };
  }

  if (details === undefined || details === null) {
    console.log("skipping invalid sample, missing details", event);
    return;
  }

  if (!from || !target) {
    return undefined;
  }
  const ttl = details?.SentTTL;
  if (ttl === undefined || ttl === null) {
    return undefined;
  }
  const seq = details?.Seq;
  if (seq === undefined || seq === null) {
    return undefined;
  }

  return {
    isTimeout: false,
    from: from,
    target: target,
    latencyMs: details?.RTT ? details.RTT / 1000000 : undefined,
    ttl: ttl,
    receivedTTL: details?.ReceivedPkt?.TTL ?? undefined,
    receivedSize: details?.ReceivedPkt?.Size ?? undefined,
    seq: seq,
    peer: details?.Request?.DstIP ?? undefined,
    mss: details?.ReceivedPkt?.MSS ?? undefined,
    peerRdns: details?.ReceivedPkt?.PeerRDNS?.[0] || undefined,
    peerASN: details?.ReceivedPkt?.PeerASN ?? undefined,
    peerISP: details?.ReceivedPkt?.PeerISP ?? undefined,
    peerLocation: details?.ReceivedPkt?.PeerLocation ?? undefined,
    peerExactLocation: details?.ReceivedPkt?.PeerExactLocation ?? undefined,
  };
}

function pingSampleFromEvent(event: RawPingEvent): PingSample | undefined {
  const from = event.metadata?.from || "";
  const target = event.metadata?.target || "";

  let latencyMs: number | undefined = undefined;
  const latencies = event.data?.RTTNanoSecs;
  if (latencies && latencies.length > 0) {
    latencyMs = latencies[latencies.length - 1] / 1000000;
  }
  // if you can obtain some basic information from a RawPingEvent, say, from, target, ttl, seq, etc.,
  // but no rtt, then it's probably a timeout event, when rendering as traceroute display, perhaps mark it as triple asterisk?

  const ttl = event.data?.TTL;
  const seq = event.data?.Seq;
  if (seq === undefined || seq === null) {
    console.log("skipping invalid sample, missing ttl or seq (or both)", event);
    return;
  }

  const raws = event.data?.Raw;
  const peer = raws && raws.length > 0 ? raws[raws.length - 1].Peer : undefined;
  const peerRdns =
    raws && raws.length > 0 ? raws[raws.length - 1].PeerRDNS : undefined;
  const peerRdnsLast =
    peerRdns && peerRdns.length > 0 ? peerRdns[peerRdns.length - 1] : undefined;

  const receivedTTL =
    raws && raws.length > 0 ? raws[raws.length - 1].TTL : undefined;

  return {
    isTimeout:
      raws === undefined ||
      raws === null ||
      (Array.isArray(raws) && raws.length === 0),
    from: from,
    target: target,
    latencyMs: latencyMs,
    ttl: ttl ?? 0,
    receivedSize:
      raws && raws.length > 0 ? raws[raws.length - 1].Size : undefined,
    receivedTTL: receivedTTL,
    seq: seq,
    peer: peer,
    peerRdns: peerRdnsLast,
    peerASN:
      raws && raws.length > 0 ? raws[raws.length - 1].PeerASN : undefined,
    peerLocation:
      raws && raws.length > 0 ? raws[raws.length - 1].PeerLocation : undefined,
    peerISP:
      raws && raws.length > 0 ? raws[raws.length - 1].PeerISP : undefined,
    peerExactLocation:
      raws && raws.length > 0
        ? raws[raws.length - 1].PeerExactLocation
        : undefined,
    lastHop:
      raws && raws.length > 0 ? raws[raws.length - 1].LastHop : undefined,
    pmtu: raws && raws.length > 0 ? raws[raws.length - 1].SetMTUTo : undefined,
  };
}

export type PingRequest = {
  sources: string[];
  targets: string[];
  count?: number;
  intervalMs: number; // how fast to generate icmp echo requests
  pktTimeoutMs: number; // how patient to wait for a icmp reply
  resolver?: string;

  // it is a pattern string for specifying how the agent generates icmp echo request packets
  // example values:
  // 'auto': incrementally increase the ttl from 1, until the destination is reached
  // 'auto(<number)': like 'auto', but explicitly specify the starting ttl
  // 'range(<s>;<e>;<d>)', 'range(<s>;<e>)' specify a range, or a range with step 'd'
  // '<number>', fixed number
  // (omit): agent will automatically determine the ttl, mostly used in pingging rather that tracerouting
  ttl?: string;

  ipInfoProviderName?: string;

  preferV4?: boolean;
  preferV6?: boolean;

  l4PacketType?: "icmp" | "udp" | "tcp";

  randomPayloadSize?: number;
};

export function generatePingSampleStream(
  pingReq: PingRequest
): ReadableStream<PingSample> {
  const {
    sources,
    targets,
    count,
    intervalMs,
    pktTimeoutMs,
    ttl,
    ipInfoProviderName,
    resolver,
    preferV4,
    preferV6,
    l4PacketType,
    randomPayloadSize,
  } = pingReq;

  const urlParams = new URLSearchParams();
  urlParams.set("from", sources.join(","));
  urlParams.set("targets", targets.join(","));
  if (count !== undefined && count !== null && count > 0) {
    urlParams.set("count", count.toString());
  }
  urlParams.set("intervalMs", intervalMs.toString());
  urlParams.set("pktTimeoutMs", pktTimeoutMs.toString());
  if (resolver) {
    urlParams.set("resolver", resolver);
  }

  if (ttl !== undefined && ttl !== null && ttl !== "") {
    urlParams.set("ttl", ttl);
  }

  if (ipInfoProviderName) {
    urlParams.set("ipInfoProviderName", ipInfoProviderName);
  }

  if (preferV4 !== undefined && preferV4 !== null && preferV4) {
    urlParams.set("preferV4", "true");
  }
  if (preferV6 !== undefined && preferV6 !== null && preferV6) {
    urlParams.set("preferV6", "true");
  }

  if (l4PacketType !== undefined && l4PacketType !== null) {
    urlParams.set("l4PacketType", l4PacketType);
  }

  if (
    randomPayloadSize !== undefined &&
    randomPayloadSize !== null &&
    randomPayloadSize > 0
  ) {
    urlParams.set("randomPayloadSize", randomPayloadSize.toString());
  }

  const headers = new Headers();
  headers.set("Content-Type", "application/json");

  let controlscope: {
    rawStream?: ReadableStream<Uint8Array<ArrayBuffer>> | null;
    sampleStream?: ReadableStream<PingSample> | null;
    reader?: ReadableStreamDefaultReader<PingSample> | null;
    stopped?: boolean;
  } = {};

  return new ReadableStream<PingSample>({
    start(controller) {
      fetch(`${getApiEndpoint()}/ping?${urlParams.toString()}`, {
        method: "GET",
        headers: headers,
      })
        .then((res) => res.body)
        .then((rawStream) => {
          controlscope.rawStream = rawStream;
          return rawStream
            ?.pipeThrough(new TextDecoderStream())
            .pipeThrough(new LineTokenizer())
            .pipeThrough(new JSONLineDecoder())
            .pipeThrough(
              l4PacketType === "tcp"
                ? new TCPPingEventAdapter()
                : new PingEventAdapter()
            );
        })
        .then((maybeSampleStream) => {
          controlscope.sampleStream = maybeSampleStream;
          if (maybeSampleStream) {
            const reader = maybeSampleStream.getReader();
            controlscope.reader = reader;
            function push() {
              reader.read().then(({ done, value }) => {
                if (done) {
                  return;
                }
                if (value && !controlscope.stopped) {
                  controller.enqueue(value);
                }
                push();
              });
            }
            push();
          }
        });
    },
    cancel() {
      console.log("[dbg] cancel stream", sources, targets);
      controlscope.stopped = true;
      controlscope.reader
        ?.cancel()
        .then(() => {
          console.log("[dbg] reader cancelled");
        })
        .catch((err) => {
          console.error("[dbg] failed to cancel reader:", err);
        });
    },
  });
}

export type ConnEntry = {
  connected_at?: number;
  last_heartbeat?: number;
  node_name?: string;
  registered_at?: number;
  attributes?: {
    [key: string]: string;
  };
};

export type Conns = {
  [key: string]: ConnEntry;
};

export async function getNodes(): Promise<Conns> {
  return fetch(`${getApiEndpoint()}/conns`).then(
    (res) => res.json() as Promise<Conns>
  );
}
