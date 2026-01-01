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
  Fragment,
  RefObject,
  useEffect,
  useMemo,
  useReducer,
  useRef,
  useState,
} from "react";
import { TaskCloseIconButton } from "@/components/taskclose";
import { PlayPauseButton, StopButton } from "./playpause";
import {
  colorGreen,
  colorRed,
  colorYellow,
  getLatencyColor,
} from "./colorfunc";
import { IPDisp } from "./ipdisp";
import { generatePingSampleStream, PingSample } from "@/apis/globalping";
import { PendingTask } from "@/apis/types";
import { demoPingSamples } from "@/apis/mock/mocktraceroute";
import { useCanvasSizing, WorldMap } from "./worldmap";
import MapIcon from "@mui/icons-material/Map";

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
};

type TabState = {
  maxHop: number;
  hopEntries: Record<number, HopEntryState>;
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
  hopEntryState: HopEntryState,
  pingSample: PingSample
): HopEntryState {
  const newEntry = { ...hopEntryState };
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

  return newEntry;
}

function updateTabState(tabState: TabState, pingSample: PingSample): TabState {
  const newTabState = { ...tabState };

  if (pingSample.ttl !== undefined && pingSample.ttl !== null) {
    newTabState.hopEntries = {
      ...newTabState.hopEntries,
      [pingSample.ttl]: updateHopEntryState(
        newTabState.hopEntries[pingSample.ttl] ?? {
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
        pingSample
      ),
    };
    if (pingSample.lastHop) {
      newTabState.maxHop = pingSample.ttl;
    }
  }

  return newTabState;
}

function updatePageState(
  pageState: PageState,
  pingSample: PingSample
): PageState {
  // debugger;
  const newState = { ...pageState };

  if (!(pingSample.from in newState)) {
    newState[pingSample.from] = {
      maxHop: 1,
      hopEntries: {},
    };
  }

  newState[pingSample.from] = updateTabState(
    newState[pingSample.from],
    pingSample
  );
  return newState;
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
          l3PacketType: !!task.useUDP ? "udp" : "icmp",
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
  const { canvasSvgRef } = useCanvasSizing(canvasW, canvasH, showMap);

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
            markers={
              [
                // {
                //   lonLat: [120.585, 31.299],
                //   fill: colorGreen,
                //   radius: 2000,
                //   strokeWidth: 800,
                //   stroke: "white",
                //   tooltip: (
                //     <Box>
                //       <Box>TTL:&nbsp;1</Box>
                //       <Box>shanghai.example.com</Box>
                //     </Box>
                //   ),
                //   index: "1",
                // },
                // {
                //   lonLat: [114.177, 22.319],
                //   fill: colorGreen,
                //   radius: 2000,
                //   strokeWidth: 800,
                //   stroke: "white",
                //   tooltip: (
                //     <Box>
                //       <Box>TTL:&nbsp;2</Box>
                //       <Box>hongkong.example.com</Box>
                //     </Box>
                //   ),
                //   index: "2",
                // },
                // {
                //   lonLat: [-118.408, 33.942],
                //   fill: colorYellow,
                //   radius: 2000,
                //   strokeWidth: 800,
                //   stroke: "white",
                //   tooltip: (
                //     <Box>
                //       <Box>TTL:&nbsp;3</Box>
                //       <Box>lax.example.com</Box>
                //     </Box>
                //   ),
                //   index: "3",
                // },
              ]
            }
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
            <IconButton onClick={() => setShowMap(!showMap)}>
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
                      <Box
                        sx={{
                          display: "grid",
                          gridTemplateColumns: "repeat(4, auto)",
                          alignItems: "center",
                          justifyItems: "flex-start",
                          justifyContent: "start",
                          columnGap: 2,
                        }}
                      >
                        {entry.peers.map((peer, idx) => (
                          <Fragment key={idx}>
                            <IPDisp rdns={peer.ip.rdns} ip={peer.ip.ip} />
                            <Box>{peer.asn}</Box>
                            <Box>{peer.location}</Box>
                            <Box>{peer.isp}</Box>
                          </Fragment>
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
