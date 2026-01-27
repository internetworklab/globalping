"use client";

import {
  Box,
  Typography,
  Table,
  TableHead,
  TableRow,
  TableCell,
  TableBody,
  TableContainer,
  Tab,
  Tabs,
  Card,
  Tooltip,
  IconButton,
  CssBaseline,
} from "@mui/material";
import {
  CSSProperties,
  Fragment,
  ReactNode,
  useEffect,
  useRef,
  useState,
} from "react";
import { TaskCloseIconButton } from "@/components/taskclose";
import { StopButton } from "./playpause";
import { getLatencyColor } from "./colorfunc";
import { IPDisp } from "./ipdisp";
import {
  ConnEntry,
  generatePingSampleStream,
  getNodes,
  NodeAttrASN,
  NodeAttrCityName,
  NodeAttrCountryCode,
  NodeAttrDN42ASN,
  NodeAttrDN42ISP,
  NodeAttrISP,
  PingSample,
} from "@/apis/globalping";
import { PendingTask } from "@/apis/types";
import {
  LonLat,
  Marker,
  Path,
  toGeodesicPaths,
  useCanvasSizing,
  useZoomControl,
  WorldMap,
  ZoomHintText,
} from "./worldmap";
import MapIcon from "@mui/icons-material/Map";
import { useQuery } from "@tanstack/react-query";
import { getNodeGroups } from "@/apis/utils";
import { testIP } from "./testip";
import {
  TracerouteReport,
  TracerouteReportHop,
  TracerouteReportMode,
  TracerouteReportPeer,
  TracerouteReportPreviewDialog,
} from "@/components/traceroutereport";
import ShareIcon from "@mui/icons-material/Share";

type TracerouteIPEntry = {
  ip: string;
  rdns?: string;
};

type TraceroutePeerEntry = {
  seq: number;
  ip: TracerouteIPEntry;
  asn?: string;
  location?: string;
  isp?: string;
};

// unit: milliseconds
type TracerouteRTTStatsEntry = {
  current: number;
  min: number;
  median: number;
  max: number;
  history: number[];
};

type TracerouteStatsEntry = {
  sent: number;
  replied: number;
  lost: number;
};

type HopEntryState = {
  peers: TraceroutePeerEntry[];
  rtts: TracerouteRTTStatsEntry;
  stats: TracerouteStatsEntry;

  // a map from peer to mtu
  pmtu?: Record<string, number>;
};

type TabState = {
  maxHop: number;
  hopEntries: Record<number, HopEntryState>;
  markers: Marker[];
  samples: PingSample[];
};
type PageState = Record<string, TabState>;

function sortAndDedupPeers(
  peers: TraceroutePeerEntry[]
): TraceroutePeerEntry[] {
  const sorted = [...peers].sort((a, b) => b.seq - a.seq);
  const m = new Map<string, TraceroutePeerEntry>();
  for (const peer of sorted) {
    if (m.has(peer.ip.ip)) {
      continue;
    }
    m.set(peer.ip.ip, peer);
  }
  return Array.from(m.values()).sort((a, b) => b.seq - a.seq);
}

function getMedian(history: number[]): number {
  if (history.length === 0) {
    return NaN;
  }
  if (history.length % 2 === 0) {
    const lmid_idx = history.length / 2 - 1;
    const rmid_idx = history.length / 2;
    return (history[lmid_idx] + history[rmid_idx]) / 2;
  }
  const mid_idx = Math.floor(history.length / 2);
  return history[mid_idx];
}

function updateHopEntryState(
  hopEntryState: HopEntryState | undefined | null,
  pingSample: PingSample
): HopEntryState {
  const newEntry = {
    ...(hopEntryState ?? {
      peers: [],
      rtts: {
        current: 0,
        min: Infinity,
        median: 0,
        max: -Infinity,
        history: [],
      },
      stats: {
        sent: 0,
        replied: 0,
        lost: 0,
      },
    }),
  };

  if (pingSample.latencyMs !== undefined && pingSample.latencyMs !== null) {
    newEntry.rtts.current = pingSample.latencyMs;
    if (pingSample.latencyMs < newEntry.rtts.min) {
      newEntry.rtts.min = pingSample.latencyMs;
    }
    if (pingSample.latencyMs > newEntry.rtts.max) {
      newEntry.rtts.max = pingSample.latencyMs;
    }
    newEntry.rtts.history = [...newEntry.rtts.history, pingSample.latencyMs];
    newEntry.rtts.median = getMedian(newEntry.rtts.history);
    newEntry.stats.replied++;
  } else {
    newEntry.stats.lost++;
  }
  newEntry.stats.sent++;

  if (pingSample.seq !== undefined && pingSample.seq !== null) {
    const newPeerEntry: TraceroutePeerEntry = {
      ip: {
        ip: pingSample.peer || "",
        rdns: pingSample.peerRdns,
      },
      seq: pingSample.seq,
      asn: pingSample.peerASN,
      location: pingSample.peerLocation,
      isp: pingSample.peerISP,
    };
    newEntry.peers = sortAndDedupPeers([...newEntry.peers, newPeerEntry]);
    // high seq first
    newEntry.peers.sort((a, b) => b.seq - a.seq);
  }

  if (
    pingSample.pmtu !== undefined &&
    pingSample.pmtu !== null &&
    !!pingSample.peer
  ) {
    newEntry.pmtu = {
      ...newEntry.pmtu,
      [pingSample.peer]: pingSample.pmtu,
    };
  }

  return newEntry;
}

function updateTabState(
  tabState: TabState | undefined | null,
  pingSample: PingSample
): TabState {
  const newTabState: TabState = {
    ...(tabState ?? {
      maxHop: 1,
      hopEntries: {},
      markers: [],
      samples: [],
    }),
  };

  newTabState.hopEntries = updateHopEntries(
    newTabState,
    pingSample,
    newTabState.hopEntries
  );

  newTabState.markers = updateMarkers(
    newTabState,
    pingSample,
    newTabState.markers
  );

  if (pingSample.lastHop) {
    newTabState.maxHop = pingSample.ttl;
  }

  const numSamples = newTabState.samples.length;
  newTabState.samples = [
    ...newTabState.samples,
    { ...pingSample, sequenceNo: numSamples },
  ];

  return newTabState;
}

function updateHopEntries(
  tabState: TabState,
  pingSample: PingSample,
  hopEntries: Record<number, HopEntryState>
): Record<number, HopEntryState> {
  const newHopEntries: Record<number, HopEntryState> = { ...hopEntries };

  if (pingSample.ttl !== undefined && pingSample.ttl !== null) {
    newHopEntries[pingSample.ttl] = updateHopEntryState(
      newHopEntries[pingSample.ttl],
      pingSample
    );
  }

  return newHopEntries;
}

function updatePageState(
  pageState: PageState,
  pingSample: PingSample
): PageState {
  return {
    ...pageState,
    [pingSample.from]: updateTabState(pageState[pingSample.from], pingSample),
  };
}

type DisplayEntry = {
  hop: number;
  entry: HopEntryState;
};

function getDispEntries(
  hopEntries: PageState,
  tabValue: string
): DisplayEntry[] {
  const dispEntries: DisplayEntry[] = [];
  const currentTabEntries = hopEntries[tabValue];
  if (currentTabEntries) {
    for (let i = 1; i <= currentTabEntries.maxHop; i++) {
      if (i in currentTabEntries.hopEntries) {
        dispEntries.push({
          hop: i,
          entry: currentTabEntries.hopEntries[i],
        });
      } else {
        dispEntries.push({
          hop: i,
          entry: {
            peers: [],
            rtts: {
              current: 0,
              min: Infinity,
              median: 0,
              max: -Infinity,
              history: [],
            },
            stats: {
              sent: 0,
              replied: 0,
              lost: 0,
            },
          },
        });
      }
    }
  }
  return dispEntries;
}

function updateMarkers(
  tabState: TabState,
  pingSample: PingSample,
  markers: Marker[] | undefined | null
): Marker[] {
  let newMarkers: Marker[] = [...(markers ?? [])];

  const ttl = pingSample.ttl;
  if (ttl === undefined || ttl === null) {
    return newMarkers;
  }

  const exact = pingSample.peerExactLocation;
  if (exact === undefined || exact === null) {
    return newMarkers;
  }

  const lon = exact.Longitude;
  const lat = exact.Latitude;
  if (lon === undefined || lon === null || lat === undefined || lat === null) {
    return newMarkers;
  }

  const rtt = pingSample.latencyMs;
  if (rtt === undefined || rtt === null) {
    return newMarkers;
  }

  const lonLat: LonLat = [lon, lat];
  const fill = getLatencyColor(rtt);
  const radius = 8;
  const strokeWidth = 3;
  const stroke: CSSProperties["stroke"] = "white";
  const tooltip: ReactNode = (
    <Box>
      <Box>TTL:&nbsp;{ttl}</Box>
      <Box>IP:&nbsp;{pingSample.peer}</Box>
      {pingSample.peerRdns && <Box>RDNS:&nbsp;{pingSample.peerRdns}</Box>}
    </Box>
  );
  const index = `TTL=${ttl}, IP=${pingSample.peer}`;
  if (newMarkers.find((marker) => marker.index === index)) {
    newMarkers = newMarkers.filter((marker) => marker.index !== index);
  }

  const newMarker: Marker = {
    lonLat,
    fill,
    radius,
    strokeWidth,
    stroke,
    tooltip,
    index,
    metadata: { ttl },
  };

  newMarkers.push(newMarker);

  newMarkers.sort((a, b) => {
    const ttl1 = a.metadata?.ttl;
    const ttl2 = b.metadata?.ttl;
    if (ttl1 === undefined || ttl1 === null) {
      return 1;
    }
    if (ttl2 === undefined || ttl2 === null) {
      return -1;
    }
    return ttl1 - ttl2;
  });

  return newMarkers;
}

function swapIJ<T>(arr: T[], i: number, j: number): T[] {
  const newArr = [...arr];
  const temp = newArr[i];
  newArr[i] = newArr[j];
  newArr[j] = temp;
  return newArr;
}

function updateReportPeer(
  peer: TracerouteReportPeer | undefined,
  sample: PingSample
): TracerouteReportPeer {
  const loc = sample.peerIPInfo
    ? {
        city: sample.peerIPInfo.City || "",
        countryAlpha2: sample.peerIPInfo.ISO3166Alpha2 || "",
      }
    : undefined;
  const isp = sample.peerIPInfo
    ? {
        ispName: sample.peerIPInfo.ISP || "",
        asn: sample.peerIPInfo.ASN || "",
      }
    : undefined;

  const lastRTT: number | undefined = sample.latencyMs;

  if (peer === undefined || peer === null) {
    if (sample.isTimeout) {
      return {
        timeout: true,
        ip: "",
      };
    }

    return {
      rdns: sample.peerRdns,
      ip: sample.peer || "",
      loc,
      isp,
      rtt:
        lastRTT !== undefined
          ? {
              lastMs: lastRTT,
              samples: [lastRTT],
            }
          : undefined,
      stat: {
        sent: 1,
        replies: sample.isTimeout ? 0 : 1,
      },
    };
  } else {
    peer = { ...peer };

    if (sample.isTimeout) {
      // do nothing for timeout sample
      peer.timeout = true;
      return peer;
    }

    if (loc) {
      peer.loc = loc;
    }
    if (isp) {
      peer.isp = isp;
    }
    if (lastRTT !== undefined) {
      peer.rtt = {
        ...(peer.rtt || {}),
        lastMs: lastRTT,
        samples: [...(peer.rtt?.samples || []), lastRTT],
      };
    }
    peer.stat = {
      ...(peer.stat || {}),
      sent: (peer.stat?.sent || 0) + 1,
      replies: (peer.stat?.replies || 0) + (sample.isTimeout ? 0 : 1),
    };

    return peer;
  }
}

function updateReportHop(
  hop: TracerouteReportHop,
  sample: PingSample
): TracerouteReportHop {
  hop = { ...hop };

  if (sample.isTimeout) {
    const peerIdx = hop.peers.findIndex((peer) => !!peer.timeout);
    if (peerIdx === -1) {
      hop.peers = [{ timeout: true, ip: "" }, ...hop.peers];
    } else {
      hop.peers = swapIJ(hop.peers, peerIdx, 0);
    }
  } else if (sample.peer) {
    const peerIdx = hop.peers.findIndex((peer) => peer.ip === sample.peer);
    if (peerIdx === -1) {
      hop.peers = [updateReportPeer(undefined, sample), ...hop.peers];
    } else {
      // swapIJ() always create a new array, so it's still immutable.
      hop.peers = swapIJ(hop.peers, peerIdx, 0);
      hop.peers[0] = updateReportPeer(hop.peers[peerIdx], sample);
    }
  }

  return hop;
}

function DisplayCurrentNode(props: {
  currentNode: ConnEntry | undefined;
  target: string;
}) {
  const { currentNode, target } = props;
  if (!currentNode) {
    return <Fragment></Fragment>;
  }
  const targetAttributes = testIP(target);
  const isNeo = targetAttributes.isNeoIP || targetAttributes.isNeoDomain;
  const isDN42 = targetAttributes.isDN42IP || targetAttributes.isDN42Domain;

  const city: string | undefined = currentNode?.attributes?.[NodeAttrCityName];
  const countryAlpha2: string | undefined =
    currentNode?.attributes?.[NodeAttrCountryCode];
  const ispASN: string | undefined = currentNode?.attributes?.[NodeAttrASN];
  const ispName: string | undefined = currentNode?.attributes?.[NodeAttrISP];
  const dn42ISPASN: string | undefined =
    currentNode?.attributes?.[NodeAttrDN42ASN];
  const dn42ISPName: string | undefined =
    currentNode?.attributes?.[NodeAttrDN42ISP];

  if (isNeo || isDN42) {
    return (
      <Box sx={{ padding: 2 }}>
        <Typography variant="body2">
          From:{" "}
          {[
            ["Node", currentNode.node_name?.toUpperCase()],
            [dn42ISPASN, dn42ISPName],
            [city, countryAlpha2],
          ]
            .map((word) => word.filter((s) => !!s).join(" "))
            .join(", ")}
        </Typography>
        <Typography variant="body2">To: {target}</Typography>
      </Box>
    );
  }
  return (
    <Box sx={{ padding: 2 }}>
      <Typography variant="body2">
        From:{" "}
        {[
          ["Node", currentNode.node_name?.toUpperCase()],
          [ispASN, ispName],
          [city, countryAlpha2],
        ]
          .map((word) => word.filter((s) => !!s).join(" "))
          .join(", ")}
      </Typography>
      <Typography variant="body2">To: {target}</Typography>
    </Box>
  );
}

function getTracerouteHops(samples: PingSample[]): TracerouteReportHop[] {
  const samplesByTTL: Record<string, PingSample[]> = {};
  for (const sample of samples) {
    const ttl = sample.ttl;
    if (ttl === undefined || ttl === null || ttl === 0) {
      continue;
    }
    if (sample.sequenceNo === undefined || sample.sequenceNo === null) {
      continue;
    }
    if (!(ttl in samplesByTTL)) {
      samplesByTTL[ttl] = [];
    }
    samplesByTTL[ttl].push(sample);
  }

  type SamplesGroup = {
    ttl: number;
    samples: PingSample[];
  };

  const samplesGroups: SamplesGroup[] = [];

  for (const key in samplesByTTL) {
    try {
      const ttl = parseInt(key);
      if (Number.isNaN(ttl) || !Number.isFinite(ttl) || ttl <= 0) {
        continue;
      }
      const samples = [...samplesByTTL[key]];
      samples.sort((a, b) => a.sequenceNo! - b.sequenceNo!);
      samplesGroups.push({
        ttl: ttl,
        samples,
      });
    } catch (e) {
      console.error(e);
    }
  }

  samplesGroups.sort((a, b) => a.ttl - b.ttl);

  const hopsData: TracerouteReportHop[] = [];

  for (const samplesGroup of samplesGroups) {
    let hopData: TracerouteReportHop = {
      ttl: samplesGroup.ttl,
      peers: [],
    };

    for (const sample of samplesGroup.samples) {
      hopData = updateReportHop(hopData, sample);
    }

    hopsData.push(hopData);
  }

  return hopsData;
}

function buildTracerouteReport(
  samples: PingSample[],
  from: ConnEntry,
  target: string,
  mode: TracerouteReportMode
): TracerouteReport {
  const now = new Date();
  const targetAttributes = testIP(target);
  const isNeo = targetAttributes.isNeoIP || targetAttributes.isNeoDomain;
  const isDN42 = targetAttributes.isDN42IP || targetAttributes.isDN42Domain;
  const dn42ISP = from?.attributes?.[NodeAttrDN42ISP];
  const dn42ASN = from?.attributes?.[NodeAttrDN42ASN];
  const isp = from?.attributes?.[NodeAttrISP];
  const asn = from?.attributes?.[NodeAttrASN];

  let usedISP = "";
  let usedASN = "";
  if (isNeo || isDN42) {
    usedISP = dn42ISP || "unknown";
    usedASN = dn42ASN || "unknown";
  } else {
    usedISP = isp || "unknown";
    usedASN = asn || "unknown";
  }

  const report: TracerouteReport = {
    date: now.valueOf(),
    sources: [
      {
        nodeName: from.node_name?.toUpperCase() || "unknown",
        isp: {
          ispName: usedISP,
          asn: usedASN,
        },
        loc: {
          city: from?.attributes?.[NodeAttrCityName] || "unknown",
          countryAlpha2: from?.attributes?.[NodeAttrCountryCode] || "unknown",
        },
      },
    ],
    destination: target,
    mode: mode,
    hops: getTracerouteHops(samples),
  };

  return report;
}

export function TracerouteResultDisplay(props: {
  task: PendingTask;
  onDeleted: () => void;
}) {
  const { task, onDeleted } = props;

  const initPageState: PageState = {};
  const pageStateRef = useRef<PageState>(initPageState);
  const [pageState, setPageState] = useState<PageState>(initPageState);

  const [paused, setPaused] = useState<boolean>(false);
  const [stopped, setStopped] = useState<boolean>(false);

  const pausedRef = useRef<boolean>(false);

  const [tabValue, setTabValue] = useState(task.sources[0]);

  const readerRef = useRef<ReadableStreamDefaultReader<PingSample> | null>(
    null
  );
  const streamRef = useRef<ReadableStream<PingSample> | null>(null);

  const [report, setReport] = useState<TracerouteReport | undefined>(undefined);

  useEffect(() => {
    let timer: number | null = null;

    if (!stopped) {
      timer = window.setTimeout(() => {
        const stream = generatePingSampleStream({
          sources: task.sources,
          targets: task.targets.slice(0, 1),
          intervalMs: 300,
          pktTimeoutMs: 3000,
          ttl: "auto",
          resolver: "172.20.0.53:53",
          ipInfoProviderName: "auto",
          preferV4: task.preferV4,
          preferV6: task.preferV6,
          l4PacketType: !!task.useUDP ? "udp" : "icmp",
          randomPayloadSize: task.pmtu ? 1500 : undefined,
        });
        streamRef.current = stream;
        const reader = stream.getReader();

        readerRef.current = reader;
        const readNext = ({
          done,
          value,
        }: {
          done: boolean;
          value: PingSample | undefined | null;
        }) => {
          if (pausedRef.current) {
            return;
          }

          if (done) {
            return;
          }

          if (value) {
            pageStateRef.current = updatePageState(pageStateRef.current, value);

            // in StrictMode, this (as most React library functions), will be called twice per sample
            setPageState(pageStateRef.current);

            readerRef.current?.read().then(readNext as any);
          }
        };
        readerRef.current?.read().then(readNext as any);
      }, 100);
    }

    return () => {
      if (timer !== null) {
        window.clearTimeout(timer);
      }

      const reader = readerRef.current;
      readerRef.current = null;
      reader
        ?.cancel()
        .then(() => {
          reader.releaseLock();
        })
        .catch((err) => {
          console.error("failed to cancel reader:", err);
        });
      const stream = streamRef.current;
      stream?.cancel().catch((e) => {
        console.error("failed to cancel stream:", e);
      });
      streamRef.current = null;
    };
  }, [task.taskId, stopped, paused]);

  const canvasW = 360000;
  const canvasH = 200000;

  const [showMap, setShowMap] = useState<boolean>(false);

  const { zoomEnabled } = useZoomControl();

  const { canvasSvgRef, ratio = 1 } = useCanvasSizing(
    canvasW,
    canvasH,
    showMap,
    zoomEnabled
  );

  const { data: conns } = useQuery({
    queryKey: ["nodes"],
    queryFn: () => getNodes(),
  });
  const sourceSet = new Set<string>([tabValue]);
  const nodeGroups = getNodeGroups(conns || {}, sourceSet);
  const sourceMarkers: Marker[] = [];
  if (nodeGroups && nodeGroups.length > 0) {
    const node = nodeGroups[0];
    if (node && node.latLon) {
      sourceMarkers.push({
        lonLat: [node.latLon[1], node.latLon[0]],
        fill: "blue",
        radius: 8,
        strokeWidth: 3,
        stroke: "white",
        index: `(SRC)`,
      });
    }
  }

  const currentNode: ConnEntry | undefined = Array.from(
    Object.entries(conns || {})
  ).find(([_, connent]) => connent.node_name === tabValue)?.[1];

  // console.log("[dbg] currentNode:", currentNode);
  // console.log("[dbg] tab:", tabValue);
  // console.log("[dbg] samples:", pageState?.[tabValue]?.samples);

  let markers: Marker[] = [];
  let extraPaths: Path[] | undefined = undefined;
  if (showMap) {
    markers = pageState?.[tabValue]?.markers || [];
    markers = [...sourceMarkers, ...markers].map((m) => ({
      ...m,
      radius: m.radius ? m.radius * ratio : undefined,
      strokeWidth: m.strokeWidth ? m.strokeWidth * ratio : undefined,
    }));
    if (markers.length > 1) {
      extraPaths = [];
      for (let j = 1; j < markers.length; j++) {
        const fromMarker = markers[j - 1];
        const toMarker = markers[j];
        if (fromMarker && toMarker && fromMarker.lonLat && toMarker.lonLat) {
          const paths = toGeodesicPaths(
            [fromMarker.lonLat[1], fromMarker.lonLat[0]],
            [toMarker.lonLat[1], toMarker.lonLat[0]],
            200
          );
          for (const path of paths) {
            extraPaths.push({
              ...path,
              stroke: "green",
              strokeWidth: 4 * ratio,
            });
          }
        }
      }
    }
  }
  const [reportGenerating, setReportGenerating] = useState<boolean>(false);

  const worldMapFill: CSSProperties["fill"] = "#676767";

  return (
    <Card>
      <Box
        sx={{
          display: "flex",
          justifyContent: "space-between",
          alignItems: "center",
          overflow: "hidden",
          padding: 2,
        }}
      >
        <Box sx={{ display: "flex", gap: 1, alignItems: "center" }}>
          <Typography variant="h6">Task #{task.taskId}</Typography>
          {task.sources.length > 0 && (
            <Tabs
              value={tabValue}
              onChange={(event, newValue) => setTabValue(newValue)}
            >
              {task.sources.map((source, idx) => (
                <Tab value={source} label={source} key={idx} />
              ))}
            </Tabs>
          )}
        </Box>
        <Box sx={{ display: "flex", gap: 1, alignItems: "center" }}>
          <Tooltip title={showMap ? "Hide Map" : "Show Map"}>
            <IconButton
              sx={{
                visibility:
                  pageState?.[tabValue]?.markers &&
                  pageState[tabValue].markers.length > 0
                    ? "visible"
                    : "hidden",
              }}
              onClick={() => setShowMap(!showMap)}
            >
              <MapIcon />
            </IconButton>
          </Tooltip>

          <Tooltip title="Share Report">
            <IconButton
              loading={reportGenerating}
              onClick={() => {
                const from = currentNode;
                const target = task?.targets?.at(0) || "";
                if (from && target) {
                  const samples = pageState?.[tabValue]?.samples || [];
                  setReportGenerating(true);
                  console.log("[dbg] gen reports, samples:", samples);
                  new Promise<TracerouteReport>((resolve) => {
                    const report = buildTracerouteReport(
                      samples,
                      from,
                      target,
                      !!task?.useUDP ? "udp" : "icmp"
                    );
                    resolve(report);
                  })
                    .then((report) => setReport(report))
                    .finally(() => setReportGenerating(false));
                }
              }}
            >
              <ShareIcon />
            </IconButton>
          </Tooltip>

          <StopButton
            stopped={stopped}
            onToggle={(prev, nxt) => {
              setStopped(nxt);
            }}
          />

          <TaskCloseIconButton
            taskId={task.taskId}
            onConfirmedClosed={() => {
              onDeleted();
            }}
          />
        </Box>
      </Box>

      <DisplayCurrentNode
        currentNode={currentNode}
        target={task?.targets?.at(0) || ""}
      />

      {showMap && (
        <Box
          sx={{
            height: showMap ? "360px" : "36px",
            position: "relative",
            top: 0,
            left: 0,
          }}
        >
          <WorldMap
            canvasSvgRef={canvasSvgRef as any}
            canvasWidth={canvasW}
            canvasHeight={canvasH}
            fill={worldMapFill}
            paths={extraPaths}
            markers={markers}
          />

          <Box
            sx={{
              display: "flex",
              justifyContent: "space-between",
              flexWrap: "wrap",
              gap: 2,
              position: "absolute",
              bottom: 0,
              alignItems: "center",
              padding: 2,
              width: "100%",
              fontSize: 12,
            }}
          >
            Traceroute to {task.targets[0]}, for informational purposes only.
          </Box>

          {!zoomEnabled && (
            <Box
              sx={{
                position: "absolute",
                top: 2,
                left: 2,
                fontSize: 12,
                padding: 2,
              }}
            >
              <ZoomHintText />
            </Box>
          )}
        </Box>
      )}

      <TableContainer sx={{ maxWidth: "100%", overflowX: "auto" }}>
        <Table>
          <TableHead>
            <TableRow>
              <TableCell>Hop</TableCell>
              <TableCell>Peers</TableCell>
              <TableCell>RTT</TableCell>
              <TableCell>Stats</TableCell>
            </TableRow>
          </TableHead>
          <TableBody>
            {getDispEntries(pageState, tabValue).map(({ hop, entry }) => {
              return (
                <TableRow key={hop}>
                  <TableCell>{hop}</TableCell>
                  <TableCell>
                    {entry.peers.length > 0 ? (
                      <Box>
                        {entry.peers.map((peer, idx) => (
                          <Box
                            sx={{
                              display: "flex",
                              gap: 1,
                              alignItems: "center",
                              flexWrap: "wrap",
                            }}
                            key={idx}
                          >
                            <IPDisp rdns={peer.ip.rdns} ip={peer.ip.ip} />
                            {!!peer.asn && <Box>{peer.asn}</Box>}
                            {!!peer.location && <Box>{peer.location}</Box>}
                            {!!peer.isp && <Box>{peer.isp}</Box>}
                            {!!entry.pmtu?.[peer.ip.ip] && (
                              <Box>PMTU={entry.pmtu[peer.ip.ip]}</Box>
                            )}
                          </Box>
                        ))}
                      </Box>
                    ) : (
                      <Box>{"***"}</Box>
                    )}
                  </TableCell>
                  <TableCell>
                    <Box sx={{ display: "flex", gap: 2, alignItems: "center" }}>
                      {entry.rtts.history.length > 0 ? (
                        <Fragment>
                          <Box
                            sx={{
                              color: getLatencyColor(entry.rtts.current),
                            }}
                          >
                            {entry.rtts.current.toFixed(3)}ms
                          </Box>
                          <Box
                            sx={{
                              display: "grid",
                              gridTemplateColumns: "repeat(3, auto)",
                              justifyContent: "space-between",
                              justifyItems: "center",
                              alignItems: "center",
                              columnGap: 2,
                            }}
                          >
                            <Fragment>
                              <Box>Min</Box>
                              <Box>Med</Box>
                              <Box>Max</Box>
                              <Box
                                sx={{
                                  color: getLatencyColor(entry.rtts.min),
                                }}
                              >
                                {entry.rtts.min.toFixed(3)}ms
                              </Box>
                              <Box
                                sx={{
                                  color: getLatencyColor(entry.rtts.median),
                                }}
                              >
                                {entry.rtts.median.toFixed(3)}ms
                              </Box>
                              <Box
                                sx={{
                                  color: getLatencyColor(entry.rtts.max),
                                }}
                              >
                                {entry.rtts.max.toFixed(3)}ms
                              </Box>
                            </Fragment>
                          </Box>
                        </Fragment>
                      ) : (
                        <Box>{"***"}</Box>
                      )}
                    </Box>
                  </TableCell>
                  <TableCell>
                    <Box sx={{ display: "flex", gap: 1, alignItems: "center" }}>
                      <Box>{entry.stats.sent} Sent,</Box>
                      <Box>{entry.stats.replied} Replied,</Box>
                      <Box>{entry.stats.lost} Lost</Box>
                    </Box>
                  </TableCell>
                </TableRow>
              );
            })}
          </TableBody>
        </Table>
      </TableContainer>
      <TracerouteReportPreviewDialog
        report={report}
        open={report !== undefined && report !== null}
        onClose={() => setReport(undefined)}
      />
    </Card>
  );
}
