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
  Button,
  IconButton,
  Tooltip,
  Card,
} from "@mui/material";
import { CSSProperties, Fragment, useEffect, useRef, useState } from "react";
import { PingSample, generatePingSampleStream } from "@/apis/globalping";
import { PendingTask } from "@/apis/types";
import { TaskCloseIconButton } from "@/components/taskclose";
import { getLatencyColor } from "./colorfunc";
import { PlayPauseButton } from "./playpause";
import MapIcon from "@mui/icons-material/Map";
import { WorldMap } from "./worldmap";

type RowObject = {
  target: string;
};

export function PingResultDisplay(props: {
  pendingTask: PendingTask;
  onDeleted: () => void;
}) {
  const { pendingTask, onDeleted } = props;
  const { sources, targets } = pendingTask;

  const [latencyMap, setLatencyMap] = useState<
    Record<string, Record<string, number>>
  >({});

  const [running, setRunning] = useState<boolean>(true);

  function launchStream(): [
    ReadableStream<PingSample>,
    ReadableStreamDefaultReader<PingSample>
  ] {
    // const resultStream = generateFakePingSampleStream(sources, targets);
    const resultStream = generatePingSampleStream({
      sources: sources,
      targets: targets,
      intervalMs: 300,
      pktTimeoutMs: 3000,
      resolver: "172.20.0.53:53",
    });
    const reader = resultStream.getReader();
    const readNext = (props: {
      done: boolean;
      value: PingSample | undefined | null;
    }) => {
      if (props.done) {
        return;
      }

      if (props.value !== undefined && props.value !== null) {
        const sample = props.value;
        const sampleFrom = sample.from;
        const sampleTarget = sample.target;
        const sampleLatency = sample.latencyMs;
        if (sampleLatency !== undefined && sampleLatency !== null) {
          setLatencyMap((prev) => ({
            ...prev,
            [sampleTarget]: {
              ...(prev[sampleTarget] || {}),
              [sampleFrom]: sampleLatency,
            },
          }));
        }
      }

      reader.read().then(readNext);
    };

    reader.read().then(readNext);
    return [resultStream, reader];
  }

  const readerRef = useRef<ReadableStreamDefaultReader<PingSample> | null>(
    null
  );

  function cancelStream() {
    if (readerRef.current) {
      const reader = readerRef.current;
      readerRef.current = null;
      reader.cancel();
    }
  }

  useEffect(() => {
    let timer: number | null = null;

    if (running) {
      timer = window.setTimeout(() => {
        const [_, reader] = launchStream();
        readerRef.current = reader;
      });
    }

    return () => {
      if (timer !== null) {
        window.clearTimeout(timer);
      }
      cancelStream();
    };
  }, [pendingTask.taskId, running]);

  const rowObjects: RowObject[] = targets.map((target) => ({
    target: target,
  }));

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
        <Typography variant="h6">Task #{pendingTask.taskId}</Typography>
        <Box sx={{ display: "flex", gap: 1, alignItems: "center" }}>
          <PlayPauseButton
            running={running}
            onToggle={(prev, nxt) => {
              if (prev) {
                cancelStream();
                setRunning(false);
              } else {
                setRunning(true);
              }
            }}
          />

          <TaskCloseIconButton
            taskId={pendingTask.taskId}
            onConfirmedClosed={() => {
              onDeleted();
            }}
          />
        </Box>
      </Box>
      <TableContainer sx={{ maxWidth: "100%", overflowX: "auto" }}>
        <Table>
          <TableHead>
            <TableRow>
              <TableCell>Target</TableCell>
              {sources.map((source) => (
                <TableCell key={source}>{source}</TableCell>
              ))}
              <TableCell>Overview</TableCell>
            </TableRow>
          </TableHead>
          <TableBody>
            {rowObjects.map(({ target }, idx) => (
              <RowMap
                key={idx}
                target={target}
                sources={sources}
                rowLength={sources.length + 2}
                latencyMap={latencyMap}
              />
            ))}
          </TableBody>
        </Table>
      </TableContainer>
    </Card>
  );
}

function RenderLegend(props: { color: CSSProperties["color"]; label: string }) {
  const { color, label } = props;
  return (
    <Box sx={{ display: "flex", alignItems: "center", gap: 1 }}>
      <Box
        sx={{
          width: 10,
          height: 10,
          backgroundColor: color,
          borderRadius: "8px",
        }}
      />
      <Typography variant="body2">{label}</Typography>
    </Box>
  );
}

function RenderLegends(props: {}) {
  return (
    <Box sx={{ paddingTop: 2, paddingRight: 2, flexShrink: 0 }}>
      <Box sx={{ display: "flex", flexDirection: "column", gap: 1 }}>
        <RenderLegend color="green" label="0-40ms" />
        <RenderLegend color="yellow" label="40-150ms" />
        <RenderLegend color="red" label="150ms+" />
        <RenderLegend color="grey" label="No Data" />
      </Box>
    </Box>
  );
}

function RowMap(props: {
  target: string;
  sources: string[];
  rowLength: number;
  latencyMap: Record<string, Record<string, number>>;
}) {
  const { target, sources, rowLength, latencyMap } = props;
  const [expanded, setExpanded] = useState<boolean>(false);
  const getLatency = (
    source: string,
    target: string
  ): number | undefined | null => {
    return latencyMap[target]?.[source];
  };

  const canvasX = 360000;
  const canvasY = 200000;

  return (
    <Fragment>
      <TableRow>
        <TableCell>{target}</TableCell>
        {sources.map((source) => {
          const latency = getLatency(source, target);
          return (
            <TableCell
              key={source}
              sx={{
                color: getLatencyColor(latency),
                fontWeight: 500,
                minWidth: 100,
              }}
            >
              {latency !== null && latency !== undefined
                ? `${latency.toFixed(3)} ms`
                : "—"}
            </TableCell>
          );
        })}
        <TableCell>
          <Tooltip
            title={
              !expanded
                ? "See Overview in World Map"
                : "Hide World Map Overview"
            }
          >
            <IconButton onClick={() => setExpanded((prev) => !prev)}>
              <MapIcon />
            </IconButton>
          </Tooltip>
        </TableCell>
      </TableRow>
      {expanded && (
        <TableRow sx={{ "&>.MuiTableCell-body": { padding: 0 } }}>
          <TableCell colSpan={rowLength}>
            <Box
              sx={{
                display: "flex",
                height: "400px",
                flexDirection: "row",
                gap: 2,
              }}
            >
              <WorldMap
                canvasX={canvasX}
                canvasY={canvasY}
                fill="lightblue"
                viewBox={`${canvasX * 0.1} ${canvasY * 0.1} 360000 ${
                  canvasY * 0.6
                }`}
                markers={[
                  {
                    lonLat: [-118.2437, 34.0522],
                    fill: "green",
                    radius: 2400,
                    strokeWidth: 1000,
                    stroke: "white",
                  },
                ]}
              />
              <RenderLegends />
            </Box>
          </TableCell>
        </TableRow>
      )}
    </Fragment>
  );
}
