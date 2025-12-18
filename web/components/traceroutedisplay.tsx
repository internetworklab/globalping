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
    // Demo samples so you can see how the traceroute table renders.
    // This emits a small, realistic-looking "stream" (multiple hops + a few timeouts).
    const demoPingSamples: PingSample[] = [
      // agent1: West coast US → 1.1.1.1 (Cloudflare)
      {
        from: "agent1",
        target: "1.1.1.1",
        ttl: 1,
        seq: 1,
        latencyMs: 1.132,
        peer: "192.168.1.1",
        peerRdns: "home-gw.lan",
        peerASN: "AS0",
        peerLocation: "San Jose, US",
        peerISP: "Local Gateway",
        peerExactLocation: { Latitude: 37.3382, Longitude: -121.8863 },
      },
      {
        from: "agent1",
        target: "1.1.1.1",
        ttl: 1,
        seq: 2,
        latencyMs: 1.481,
        peer: "192.168.1.1",
        peerRdns: "home-gw.lan",
        peerASN: "AS0",
        peerLocation: "San Jose, US",
        peerISP: "Local Gateway",
        peerExactLocation: { Latitude: 37.3382, Longitude: -121.8863 },
      },
      {
        from: "agent1",
        target: "1.1.1.1",
        ttl: 2,
        seq: 3,
        latencyMs: 4.902,
        peer: "10.10.0.1",
        peerRdns: "cmts01.isp.example",
        peerASN: "AS64512",
        peerLocation: "San Jose, US",
        peerISP: "ExampleCable",
        peerExactLocation: { Latitude: 37.3382, Longitude: -121.8863 },
      },
      {
        from: "agent1",
        target: "1.1.1.1",
        ttl: 3,
        seq: 4,
        latencyMs: 10.774,
        peer: "203.0.113.9",
        peerRdns: "edge01.sjc.example.net",
        peerASN: "AS64512",
        peerLocation: "San Jose, US",
        peerISP: "ExampleCable",
        peerExactLocation: { Latitude: 37.3382, Longitude: -121.8863 },
      },
      {
        from: "agent1",
        target: "1.1.1.1",
        ttl: 4,
        seq: 5,
        latencyMs: 16.221,
        peer: "198.51.100.14",
        peerRdns: "core01.sfo.example.net",
        peerASN: "AS64512",
        peerLocation: "San Francisco, US",
        peerISP: "ExampleCable",
        peerExactLocation: { Latitude: 37.7749, Longitude: -122.4194 },
      },
      // timeout probe at hop 5 (no latency + no peer/seq so it renders as loss only)
      { from: "agent1", target: "1.1.1.1", ttl: 5 },
      {
        from: "agent1",
        target: "1.1.1.1",
        ttl: 5,
        seq: 6,
        latencyMs: 21.093,
        peer: "162.158.88.1",
        peerRdns: "ae1.cloudflare-sfo.example",
        peerASN: "AS13335",
        peerLocation: "San Francisco, US",
        peerISP: "Cloudflare",
        peerExactLocation: { Latitude: 37.7749, Longitude: -122.4194 },
      },
      {
        from: "agent1",
        target: "1.1.1.1",
        ttl: 6,
        seq: 7,
        latencyMs: 22.611,
        peer: "1.1.1.1",
        peerRdns: "one.one.one.one",
        peerASN: "AS13335",
        peerLocation: "San Francisco, US",
        peerISP: "Cloudflare",
        peerExactLocation: { Latitude: 37.7749, Longitude: -122.4194 },
      },

      // agent2: London → 8.8.8.8 (Google DNS)
      {
        from: "agent2",
        target: "8.8.8.8",
        ttl: 1,
        seq: 1,
        latencyMs: 0.842,
        peer: "192.168.0.1",
        peerRdns: "router.lan",
        peerASN: "AS0",
        peerLocation: "London, UK",
        peerISP: "Local Gateway",
        peerExactLocation: { Latitude: 51.5072, Longitude: -0.1276 },
      },
      {
        from: "agent2",
        target: "8.8.8.8",
        ttl: 2,
        seq: 2,
        latencyMs: 6.401,
        peer: "100.64.0.1",
        peerRdns: "bnG01.isp.example",
        peerASN: "AS64513",
        peerLocation: "London, UK",
        peerISP: "ExampleFiber",
        peerExactLocation: { Latitude: 51.5072, Longitude: -0.1276 },
      },
      {
        from: "agent2",
        target: "8.8.8.8",
        ttl: 3,
        seq: 3,
        latencyMs: 11.883,
        peer: "203.0.113.25",
        peerRdns: "core01.lon.example.net",
        peerASN: "AS64513",
        peerLocation: "London, UK",
        peerISP: "ExampleFiber",
        peerExactLocation: { Latitude: 51.5072, Longitude: -0.1276 },
      },
      // a brief loss at hop 4
      { from: "agent2", target: "8.8.8.8", ttl: 4 },
      {
        from: "agent2",
        target: "8.8.8.8",
        ttl: 4,
        seq: 4,
        latencyMs: 15.992,
        peer: "198.51.100.41",
        peerRdns: "ixp01.lon.example",
        peerASN: "AS65500",
        peerLocation: "London, UK",
        peerISP: "LINX",
        peerExactLocation: { Latitude: 51.5072, Longitude: -0.1276 },
      },
      {
        from: "agent2",
        target: "8.8.8.8",
        ttl: 5,
        seq: 5,
        latencyMs: 19.204,
        peer: "142.250.214.193",
        peerRdns: "lhr25s12-in-f1.1e100.net",
        peerASN: "AS15169",
        peerLocation: "London, UK",
        peerISP: "Google",
        peerExactLocation: { Latitude: 51.5072, Longitude: -0.1276 },
      },
      {
        from: "agent2",
        target: "8.8.8.8",
        ttl: 6,
        seq: 6,
        latencyMs: 19.882,
        peer: "8.8.8.8",
        peerRdns: "dns.google",
        peerASN: "AS15169",
        peerLocation: "London, UK",
        peerISP: "Google",
        peerExactLocation: { Latitude: 51.5072, Longitude: -0.1276 },
      },

      // agent3: Singapore → 9.9.9.9 (Quad9)
      {
        from: "agent3",
        target: "9.9.9.9",
        ttl: 1,
        seq: 1,
        latencyMs: 1.004,
        peer: "192.168.88.1",
        peerRdns: "ap-gw.lan",
        peerASN: "AS0",
        peerLocation: "Singapore, SG",
        peerISP: "Local Gateway",
        peerExactLocation: { Latitude: 1.3521, Longitude: 103.8198 },
      },
      {
        from: "agent3",
        target: "9.9.9.9",
        ttl: 2,
        seq: 2,
        latencyMs: 4.771,
        peer: "10.20.0.1",
        peerRdns: "bng01.isp.example",
        peerASN: "AS64514",
        peerLocation: "Singapore, SG",
        peerISP: "ExampleMobile",
        peerExactLocation: { Latitude: 1.3521, Longitude: 103.8198 },
      },
      {
        from: "agent3",
        target: "9.9.9.9",
        ttl: 3,
        seq: 3,
        latencyMs: 8.993,
        peer: "203.0.113.77",
        peerRdns: "core01.sin.example.net",
        peerASN: "AS64514",
        peerLocation: "Singapore, SG",
        peerISP: "ExampleMobile",
        peerExactLocation: { Latitude: 1.3521, Longitude: 103.8198 },
      },
      {
        from: "agent3",
        target: "9.9.9.9",
        ttl: 4,
        seq: 4,
        latencyMs: 36.202,
        peer: "198.51.100.88",
        peerRdns: "sg-ix01.example",
        peerASN: "AS65501",
        peerLocation: "Singapore, SG",
        peerISP: "SGIX",
        peerExactLocation: { Latitude: 1.3521, Longitude: 103.8198 },
      },
      {
        from: "agent3",
        target: "9.9.9.9",
        ttl: 5,
        seq: 5,
        latencyMs: 172.511,
        peer: "45.90.28.0",
        peerRdns: "anycast.quad9.net",
        peerASN: "AS19281",
        peerLocation: "Zurich, CH",
        peerISP: "Quad9",
        peerExactLocation: { Latitude: 47.3769, Longitude: 8.5417 },
      },
      // final hop
      {
        from: "agent3",
        target: "9.9.9.9",
        ttl: 6,
        seq: 6,
        latencyMs: 173.044,
        peer: "9.9.9.9",
        peerRdns: "dns.quad9.net",
        peerASN: "AS19281",
        peerLocation: "Zurich, CH",
        peerISP: "Quad9",
        peerExactLocation: { Latitude: 47.3769, Longitude: 8.5417 },
      },
    ];

    const timeoutIds: number[] = [];
    const baseDelayMs = 300;
    for (let i = 0; i < demoPingSamples.length; i++) {
      const tid = window.setTimeout(() => {
        setHopEntries((prev) => updatePageState(prev, demoPingSamples[i]));
      }, baseDelayMs * (i + 1));
      timeoutIds.push(tid);
    }

    return () => {
      timeoutIds.forEach((tid) => window.clearTimeout(tid));
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
