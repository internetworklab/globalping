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
} from "@mui/material";
import { Fragment, useEffect, useState } from "react";
import { TaskCloseIconButton } from "@/components/taskclose";
import { PlayPauseButton } from "./playpause";
import { getLatencyColor } from "./colorfunc";
import { IPDisp } from "./ipdisp";
import { PingSample } from "@/apis/globalping";

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
    newEntry.peers = [...newEntry.peers, newPeerEntry];
    // high seq first
    newEntry.peers.sort((a, b) => b.seq - a.seq);
  }

  return newEntry;
}

function updateTabState(tabState: TabState, pingSample: PingSample): TabState {
  const newTabState = { ...tabState };

  if (pingSample.ttl !== undefined && pingSample.ttl !== null) {
    if (pingSample.ttl > newTabState.maxHop) {
      newTabState.maxHop = pingSample.ttl;
    }

    newTabState.maxHop = Math.max(newTabState.maxHop, pingSample.ttl);
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
      }
    }
  }
  return dispEntries;
}

export function TracerouteResultDisplay(props: {}) {
  const [hopEntries, setHopEntries] = useState<PageState>({});

  const fakeSources = ["agent1", "agent2", "agent3"];
  const [tabValue, setTabValue] = useState(fakeSources[0]);

  useEffect(() => {
    const pingSample1: PingSample = {
      from: "agent1",
      target: "1.1.1.1",
      latencyMs: 100,
      ttl: 1,
      seq: 1,
      peer: "1.1.1.1",
      peerRdns: "one.one.one.one",
      peerASN: "AS65001",
      peerLocation: "California",
      peerISP: "ATT",
      peerExactLocation: {
        Latitude: 37.7749,
        Longitude: -122.4194,
      },
    };
    const pingSample2: PingSample = {
      from: "agent1",
      target: "1.1.1.1",
      latencyMs: 90,
      ttl: 2,
      seq: 2,
      peer: "1.1.1.2",
      peerRdns: "one.one.one.two",
      peerASN: "AS65002",
      peerLocation: "California",
      peerISP: "China Telecom",
      peerExactLocation: {
        Latitude: 34.0522,
        Longitude: -118.2437,
      },
    };

    window.setTimeout(() => {
      setHopEntries((prev) => updatePageState(prev, pingSample1));
    }, 1000);
    window.setTimeout(() => {
      setHopEntries((prev) => updatePageState(prev, pingSample2));
    }, 2000);
    return () => {
      setHopEntries({});
    };
  }, []);

  return (
    <Fragment>
      <Box
        sx={{
          display: "flex",
          justifyContent: "space-between",
          alignItems: "center",
        }}
      >
        <Box sx={{ display: "flex", gap: 1, alignItems: "center" }}>
          <Typography variant="h6">Task #{1}</Typography>
          <Tabs
            value={tabValue}
            onChange={(event, newValue) => setTabValue(newValue)}
          >
            <Tab value={fakeSources[0]} label={fakeSources[0]} />
            <Tab value={fakeSources[1]} label={fakeSources[1]} />
            <Tab value={fakeSources[2]} label={fakeSources[2]} />
          </Tabs>
        </Box>
        <Box sx={{ display: "flex", gap: 1, alignItems: "center" }}>
          <PlayPauseButton
            running={true}
            onToggle={(prev, nxt) => {
              if (prev) {
                // todo
              } else {
                // todo
              }
            }}
          />

          <TaskCloseIconButton
            taskId={"1"}
            onConfirmedClosed={() => {
              // todo
            }}
          />
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
            {getDispEntries(hopEntries, tabValue).map(({ hop, entry }) => {
              return (
                <TableRow key={hop}>
                  <TableCell>{hop}</TableCell>
                  <TableCell>
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
                  </TableCell>
                  <TableCell>
                    <Box sx={{ display: "flex", gap: 2, alignItems: "center" }}>
                      <Box sx={{ color: getLatencyColor(entry.rtts.current) }}>
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
                        <Box>Min</Box>
                        <Box>Med</Box>
                        <Box>Max</Box>
                        <Box sx={{ color: getLatencyColor(entry.rtts.min) }}>
                          {entry.rtts.min.toFixed(3)}ms
                        </Box>
                        <Box sx={{ color: getLatencyColor(entry.rtts.median) }}>
                          {entry.rtts.median.toFixed(3)}ms
                        </Box>
                        <Box sx={{ color: getLatencyColor(entry.rtts.max) }}>
                          {entry.rtts.max.toFixed(3)}ms
                        </Box>
                      </Box>
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
    </Fragment>
  );
}
