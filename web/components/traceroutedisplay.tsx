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
import { Fragment, useState } from "react";
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

type TracerouteDispEntry = {
  peers: TraceroutePeerEntry[];
  rtts: TracerouteRTTStatsEntry;
  stats: TracerouteStatsEntry;
};

const fakeEntries: TracerouteDispEntry[] = [
  {
    peers: [
      {
        seq: 3,
        ip: {
          ip: "1.1.1.1",
          rdns: "one.one.one.one",
        },
        asn: "AS65001",
        location: "California",
        isp: "China Telecom",
      },
      {
        seq: 2,
        ip: {
          ip: "2.2.2.2",
        },
        asn: "AS65002",
        location: "London",
        isp: "British Telecom",
      },
    ],
    rtts: {
      current: 13.123,
      min: 10.101,
      median: 12.121,
      max: 14.141,
      history: [10.101, 12.121, 14.141],
    },
    stats: {
      sent: 10,
      replied: 8,
      lost: 2,
    },
  },
];

type TabState = {
  maxHop: number;
  hopEntries: Record<number, TracerouteDispEntry>;
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

function updatePageState(
  pageState: PageState,
  pingSample: PingSample
): PageState {
  const newState = { ...pageState };

  if (!(pingSample.from in newState)) {
    newState[pingSample.from] = {
      maxHop: 1,
      hopEntries: {},
    };
  }

  const newTabState = { ...newState[pingSample.from] };
  if (pingSample.ttl !== undefined && pingSample.ttl !== null) {
    if (pingSample.ttl > newTabState.maxHop) {
      newTabState.maxHop = pingSample.ttl;
    }

    if (!(pingSample.ttl in newTabState.hopEntries)) {
      newTabState.hopEntries[pingSample.ttl] = {
        peers: [],
        rtts: {
          current: 0,
          min: 0,
          median: 0,
          max: 0,
          history: [],
        },
        stats: {
          sent: 0,
          replied: 0,
          lost: 0,
        },
      };
    }

    const newEntry = { ...newTabState.hopEntries[pingSample.ttl] };
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

    newTabState.hopEntries[pingSample.ttl] = newEntry;
  }

  return newState;
}

type DisplayEntry = {
  hop: number;
  entry: TracerouteDispEntry;
};

export function TracerouteResultDisplay(props: {}) {
  const [hopEntries, setHopEntries] = useState<PageState>({
    agent1: {
      maxHop: 1,
      hopEntries: {
        1: fakeEntries[0],
      },
    },
  });

  const fakeSources = ["agent1", "agent2", "agent3"];
  const [tabValue, setTabValue] = useState(fakeSources[0]);

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
            {dispEntries.map(({ hop, entry }) => {
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
