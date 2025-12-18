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
} from "@mui/material";
import { Fragment, useState } from "react";
import { TaskCloseIconButton } from "@/components/taskclose";
import { PlayPauseButton } from "./playpause";
import { getLatencyColor } from "./colorfunc";
import { IPDisp } from "./ipdisp";

type TracerouteIPEntry = {
  ip: string;
  rdns?: string;
};

type TraceroutePeerEntry = {
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
        ip: {
          ip: "1.1.1.1",
          rdns: "one.one.one.one",
        },
        asn: "AS65001",
        location: "California",
        isp: "China Telecom",
      },
      {
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
    },
    stats: {
      sent: 10,
      replied: 8,
      lost: 2,
    },
  },
];

export function TracerouteResultDisplay(props: {}) {
  const [maxHop, setMaxHop] = useState(1);
  const [hopEntries, setHopEntries] = useState<
    Record<number, TracerouteDispEntry>
  >({
    1: fakeEntries[0],
  });

  const dispEntries: [number, TracerouteDispEntry][] = [];
  for (let i = 1; i <= maxHop; i++) {
    if (i in hopEntries) {
      dispEntries.push([i, hopEntries[i]]);
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
        <Typography variant="h6">Task #{1}</Typography>
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
            {dispEntries.map(([hop, entry]) => {
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
