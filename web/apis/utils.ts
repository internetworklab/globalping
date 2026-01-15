import { ConnEntry, Conns, PingSample } from "./globalping";

export function streamFromSamples(
  samples: PingSample[]
): ReadableStream<PingSample> {
  const baseDelayMs = 300;
  const jitterMs = 125;

  let closed = false;
  let timeoutIds: Array<ReturnType<typeof setTimeout>> = [];

  const clearAllTimeouts = () => {
    for (const tid of timeoutIds) {
      globalThis.clearTimeout(tid);
    }
    timeoutIds = [];
  };

  return new ReadableStream<PingSample>({
    start(controller) {
      if (samples.length === 0) {
        closed = true;
        controller.close();
        return;
      }

      timeoutIds = samples.map((sample, idx) => {
        const delayMs =
          baseDelayMs * (idx + 1) + Math.floor(Math.random() * jitterMs);
        return globalThis.setTimeout(() => {
          if (closed) {
            return;
          }
          controller.enqueue(sample);
          if (idx === samples.length - 1) {
            closed = true;
            controller.close();
          }
        }, delayMs);
      });
    },
    cancel() {
      closed = true;
      clearAllTimeouts();
    },
  });
}

export type NodeGroup = {
  nodes: ConnEntry[];
  groupName: string;
  latLon: [number, number];
};

export function getNodeLatLon(conn: ConnEntry): [number, number] | undefined {
  const locString = conn.attributes?.["ExactLocation"];
  if (locString === undefined || locString === null || locString === "") {
    return undefined;
  }
  const locs = locString.split(",");
  if (locs.length !== 2) {
    return undefined;
  }
  const lat = parseFloat(locs[0]);
  const lon = parseFloat(locs[1]);

  if (isNaN(lat) || isNaN(lon)) {
    return undefined;
  }
  if (!Number.isFinite(lat) || !Number.isFinite(lon)) {
    return undefined;
  }
  return [lat, lon];
}

export function getGridIndex(latLon: [number, number]): [number, number] {
  const lat = latLon[0];
  const lon = latLon[1];
  const latIndex = Math.floor(lat / 12);
  const lonIndex = Math.floor(lon / 12);
  return [latIndex, lonIndex];
}

export function getGridKey(latLon: [number, number]): string {
  const [latIndex, lonIndex] = getGridIndex(latLon);
  return `${latIndex},${lonIndex}`;
}

export function getNodeGroups(
  conns: Conns,
  sourceSet: Set<string>
): NodeGroup[] {
  const groups: Record<string, NodeGroup> = {};
  for (const connKey in conns) {
    const connEntry = conns[connKey];

    if (connEntry.node_name && !sourceSet.has(connEntry.node_name)) {
      continue;
    }

    const latLon = getNodeLatLon(connEntry);
    if (latLon === undefined) {
      continue;
    }
    const gridKey = getGridKey(latLon);
    if (groups[gridKey] === undefined) {
      groups[gridKey] = {
        nodes: [],
        groupName: gridKey,
        latLon: latLon,
      };
    }
    groups[gridKey].nodes.push(connEntry);
  }
  return Array.from(Object.values(groups));
}
