"use client";

import {
  Box,
  Card,
  Typography,
  CardContent,
  Table,
  TableHead,
  TableRow,
  TableCell,
  TableBody,
  TextField,
} from "@mui/material";
import { Fragment, useEffect, useMemo, useState } from "react";

type PingSample = {
  from: string;
  target: string;

  // in the unit of milliseconds
  latencyMs: number;
};


const fakeSources = [
  "lax1",
  "vie1",
  "ams1",
  "dal1",
  "fra1",
  "nyc1",
  "hkg1",
  "sgp1",
  "tyo1",
];

const fakeTargets = [
  "google.com",
  "github.com",
  "8.8.8.8",
  "x.com",
  "example.com",
  "1.1.1.1",
  "223.5.5.5",
];

function PingResultDisplay(props:{ resultStream: ReadableStream<PingSample>, sources: string[], targets: string[] }) {
  

  const { sources, targets, resultStream } = props;
  

  const [latencyMap, setLatencyMap] = useState<Record<string, Record<string, number>>>({});
  const getLatency = (source: string, target: string): number | undefined | null => {
    return latencyMap[target]?.[source];
  }

  useEffect(() => {
    const reader = resultStream.getReader()

    reader.read().then(({done, value}) => {
      if (done) {
        console.log('[dbg] done, value:', value)
        return
      }

      const sample = value as PingSample
      const sampleFrom = sample.from
      const sampleTarget = sample.target
      const sampleLatency = sample.latencyMs

      setLatencyMap((prev) => ({
        ...prev,
        [sampleFrom]: {
          ...(prev[sampleFrom] || {}),
          [sampleTarget]: sampleLatency,
        }
      }))

    })

    return () => {
      reader.cancel()
    }
  })

return <Fragment>
   <Table>
            <TableHead>
              <TableRow>
                <TableCell>Target</TableCell>
                {sources.map((source) => (
                  <TableCell key={source}>{source}</TableCell>
                ))}
              </TableRow>
            </TableHead>
            <TableBody>
              {targets.map((target) => (
                <TableRow key={target}>
                  <TableCell>{target}</TableCell>
                  {sources.map((source) => {
                    const latency = getLatency(source, target);
                    return (
                      <TableCell key={source}>
                        {latency !== null ? `${latency} ms` : "—"}
                      </TableCell>
                    );
                  })}
                </TableRow>
              ))}
            </TableBody>
          </Table>
</Fragment>
}

function generateFakePingSampleStream(sources: string[], targets: string[]): ReadableStream<PingSample> {

}

export default function Home() {
  

  const pingSamples: PingSample[] = [];

  const [sources, setSources] = useState<string[]>(fakeSources);
  const [targets, setTargets] = useState<string[]>(fakeTargets);
  const [target, setTarget] = useState<string>("");

  const sampleStream = useMemo(() => generateFakePingSampleStream(sources, targets), [sources, targets]);

  return (
    <Box sx={{ padding: 2, display: "flex", flexDirection: "column", gap: 2 }}>
      <Card>
        <CardContent>
          <Typography variant="h6">GlobalPing</Typography>
          <Box sx={{ marginTop: 2 }}>
            <TextField
              variant="standard"
              fullWidth
              label="Target"
              value={target}
              onChange={(e) => setTarget(e.target.value)}
            />
          </Box>
        </CardContent>
      </Card>
      <Card>
        <CardContent>
         <PingResultDisplay resultStream={sampleStream} sources={sources} targets={targets} />
        </CardContent>
      </Card>
    </Box>
  );
}
