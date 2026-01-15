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
  generatePingSampleStream,
  getNodes,
  PingSample,
} from "@/apis/globalping";
import { PendingTask } from "@/apis/types";
import {
  LonLat,
  Marker,
  Path,
  toGeodesicPaths,
  useCanvasSizing,
  WorldMap,
} from "./worldmap";
import MapIcon from "@mui/icons-material/Map";
import { useQuery } from "@tanstack/react-query";
import { getNodeGroups } from "@/apis/utils";

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
  const newTabState = {
    ...(tabState ?? {
      maxHop: 1,
      hopEntries: {},
      markers: [],
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
  const radius = 2000;
  const strokeWidth = 800;
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

  useEffect(() => {
    console.log("[dbg] useEffect mount, stopped:", stopped);

    let timer: number | null = null;

    if (!stopped) {
      timer = window.setTimeout(() => {
        console.log("[dbg] creating stream");
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
            console.log("[dbg] paused, skipping");
            return;
          }
          console.log("[dbg] readNext", done, value);
          if (done) {
            return;
          }
          if (value) {
            console.log("[dbg] readNext value:", value);
            pageStateRef.current = updatePageState(pageStateRef.current, value);

            // in StrictMode, this will be called twice per sample
            setPageState(pageStateRef.current);

            readerRef.current?.read().then(readNext);
          }
        };
        readerRef.current?.read().then(readNext);
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
          console.log("[dbg] reader cancelled");
          reader.releaseLock();
        })
        .catch((err) => {
          console.error("[dbg] failed to cancel reader:", err);
        });
      const stream = streamRef.current;
      stream
        ?.cancel()
        .then(() => {
          console.log("[dbg] stream cancelled");
        })
        .catch(() => {});
      streamRef.current = null;
    };
  }, [task.taskId, stopped, paused]);

  const canvasW = 360000;
  const canvasH = 200000;

  const [showMap, setShowMap] = useState<boolean>(false);
  const { canvasSvgRef } = useCanvasSizing(canvasW, canvasH, showMap, false);

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
        radius: 2000,
        strokeWidth: 800,
        stroke: "white",
        index: `(SRC)`,
      });
    }
  }

  let markers: Marker[] = [];
  let extraPaths: Path[] | undefined = undefined;
  if (showMap) {
    markers = pageState?.[tabValue]?.markers || [];
    markers = [...sourceMarkers, ...markers];
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
              strokeWidth: 1000,
            });
          }
        }
      }
    }
  }

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

      <Box
        sx={{
          height: showMap ? "360px" : "36px",
          position: "relative",
          top: 0,
          left: 0,
        }}
      >
        {showMap && (
          <WorldMap
            canvasSvgRef={canvasSvgRef as any}
            canvasWidth={canvasW}
            canvasHeight={canvasH}
            fill="lightblue"
            paths={extraPaths}
            markers={markers}
          />
        )}

        <Box
          sx={{
            display: "flex",
            justifyContent: "space-between",
            flexWrap: "wrap",
            gap: 2,
            position: "absolute",
            bottom: 0,
            alignItems: "center",
            paddingLeft: 2,
            paddingRight: 2,
            width: "100%",
          }}
        >
          <Box>
            {task.targets.length > 0 && task.targets[0] && (
              <Box>
                Traceroute to {task.targets[0]}, for informational purposes
                only.
              </Box>
            )}
          </Box>
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
        </Box>
      </Box>

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
    </Card>
  );
}
