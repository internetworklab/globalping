"use client";

import { useQuery } from "@tanstack/react-query";
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
import {
  PingSample,
  generatePingSampleStream,
  getNodes,
  Conns,
  ConnEntry,
} from "@/apis/globalping";
import { PendingTask } from "@/apis/types";
import { TaskCloseIconButton } from "@/components/taskclose";
import {
  ColorEncoding,
  getLatencyColor,
  encodings,
  latencyColorEncodingToString,
  colorGrey,
} from "./colorfunc";
import { PlayPauseButton } from "./playpause";
import MapIcon from "@mui/icons-material/Map";
import { Marker, WorldMap } from "./worldmap";

type RowObject = {
  target: string;
};

export function PingResultDisplay(props: {
  pendingTask: PendingTask;
  onDeleted: () => void;
}) {
  const { pendingTask, onDeleted } = props;
  const { sources, targets, preferV4, preferV6 } = pendingTask;

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
      preferV4: preferV4,
      preferV6: preferV6,
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

function RenderLegends(props: { encodings: ColorEncoding[] }) {
  const { encodings } = props;
  return (
    <Box sx={{ paddingTop: 2, paddingRight: 2, flexShrink: 0 }}>
      <Box sx={{ display: "flex", flexDirection: "column", gap: 1 }}>
        {encodings.map((encoding) => (
          <RenderLegend
            key={encoding.range[0]}
            color={encoding.color}
            label={latencyColorEncodingToString(encoding)}
          />
        ))}
        <RenderLegend color={colorGrey} label="No Data" />
      </Box>
    </Box>
  );
}

type NodeGroup = {
  nodes: ConnEntry[];
  groupName: string;
  latLon: [number, number];
};

function getNodeLatLon(conn: ConnEntry): [number, number] | undefined {
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

function getGridIndex(latLon: [number, number]): [number, number] {
  const lat = latLon[0];
  const lon = latLon[1];
  const latIndex = Math.floor(lat / 12);
  const lonIndex = Math.floor(lon / 12);
  return [latIndex, lonIndex];
}

function getGridKey(latLon: [number, number]): string {
  const [latIndex, lonIndex] = getGridIndex(latLon);
  return `${latIndex},${lonIndex}`;
}

function getNodeGroups(conns: Conns): NodeGroup[] {
  const groups: Record<string, NodeGroup> = {};
  for (const connKey in conns) {
    const connEntry = conns[connKey];
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

function latLonToLonLat(latLon: [number, number]): [number, number] {
  return [latLon[1], latLon[0]];
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

  const { data: conns } = useQuery({
    queryKey: ["nodes"],
    queryFn: () => getNodes(),
  });

  let nodeGroups: NodeGroup[] = [];
  if (conns !== undefined && conns !== null) {
    nodeGroups = getNodeGroups(conns);
  }

  let markers: Marker[] = [];
  for (const nodeGroup of nodeGroups) {
    const latenciesMap: Record<string, number> = {};
    const latencies: number[] = [];
    for (const node of nodeGroup.nodes) {
      if (
        node.node_name === undefined ||
        node.node_name === null ||
        node.node_name === ""
      ) {
        continue;
      }
      const latency = getLatency(node.node_name, target);
      if (
        latency !== undefined &&
        latency !== null &&
        latency !== Infinity &&
        !isNaN(latency) &&
        Number.isFinite(latency) &&
        latency >= 0
      ) {
        latencies.push(latency);
        latenciesMap[node.node_name] = latency;
      }
    }
    let tooltip: string = "";
    latencies.sort((a, b) => a - b);
    let color: CSSProperties["color"] = colorGrey;
    if (latencies.length > 0) {
      color = getLatencyColor(latencies[0]);
      for (const [nodeName, latency] of Object.entries(latenciesMap)) {
        tooltip += `${nodeName}: ${latency.toFixed(3)} ms\n`;
      }
    }

    const marker: Marker = {
      lonLat: latLonToLonLat(nodeGroup.latLon),
      fill: color,
      radius: 2000,
      strokeWidth: 800,
      stroke: "white",
    };
    if (tooltip !== "") {
      marker.tooltip = tooltip;
    }
    markers.push(marker);
  }

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
                canvasWidth={canvasX}
                canvasHeight={canvasY}
                fill="lightblue"
                viewBox={`${canvasX * 0.1} ${canvasY * 0.1} 360000 ${
                  canvasY * 0.6
                }`}
                markers={markers}
              />
              <RenderLegends encodings={encodings} />
            </Box>
          </TableCell>
        </TableRow>
      )}
    </Fragment>
  );
}
