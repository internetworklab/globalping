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
  Divider,
  tooltipClasses,
  styled,
  TooltipProps,
} from "@mui/material";
import {
  CSSProperties,
  Fragment,
  useEffect,
  useMemo,
  useRef,
  useState,
} from "react";
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
import { Marker, useCanvasSizing, WorldMap } from "./worldmap";

type RowObject = {
  target: string;
};

type TableCellData = {
  latest: {
    latency: number | undefined | null;
    mss: number | undefined | null;
  };
  latestSample: PingSample | undefined | null;
  samples: PingSample[];
};

type TableCellDataMap = Record<string, Record<string, TableCellData>>;

function updateTableCellDataMap(
  prev: TableCellDataMap,
  sample: PingSample
): TableCellDataMap {
  const newMap = { ...prev };
  if (!newMap[sample.target]) {
    newMap[sample.target] = {};
  }
  if (!newMap[sample.target][sample.from]) {
    newMap[sample.target][sample.from] = {
      latest: {
        latency: sample.latencyMs,
        mss: sample.mss,
      },
      latestSample: sample,
      samples: [sample],
    };
    return newMap;
  }
  newMap[sample.target] = {
    ...newMap[sample.target],
    [sample.from]: {
      ...newMap[sample.target][sample.from],
      latest: {
        latency: sample.latencyMs,
        mss: sample.mss,
      },
      latestSample: sample,
      samples: [sample, ...newMap[sample.target][sample.from].samples],
    },
  };
  return newMap;
}

function getLatestDataFromMap(
  map: TableCellDataMap,
  target: string,
  source: string
): {
  datum: PingSample | undefined | null;
  latency: number | undefined | null;
  mss: number | undefined | null;
  historySamples: PingSample[];
} {
  const data = map[target]?.[source];
  const latest = data?.latest;
  return {
    datum: data?.latestSample,
    latency: latest?.latency,
    mss: latest?.mss,
    historySamples: data?.samples ?? [],
  };
}

export function PingResultDisplay(props: {
  pendingTask: PendingTask;
  onDeleted: () => void;
}) {
  const { pendingTask, onDeleted } = props;
  const { sources, targets, preferV4, preferV6, useUDP } = pendingTask;

  const [tableCellDataMap, setTableCellDataMap] = useState<TableCellDataMap>(
    {}
  );

  const [running, setRunning] = useState<boolean>(true);

  function launchStream(): [
    ReadableStream<PingSample>,
    ReadableStreamDefaultReader<PingSample>
  ] {
    // const resultStream = generateFakePingSampleStream(sources, targets);
    const resultStream = generatePingSampleStream({
      sources: sources,
      targets: targets,
      intervalMs: pendingTask.type === "tcpping" ? 1000 : 300,
      pktTimeoutMs: 3000,
      resolver: "172.20.0.53:53",
      preferV4: preferV4,
      preferV6: preferV6,
      l4PacketType: !!useUDP
        ? "udp"
        : pendingTask.type === "tcpping"
        ? "tcp"
        : "icmp",
      ipInfoProviderName: "auto",
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
        setTableCellDataMap((prev) => updateTableCellDataMap(prev, sample));
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
                tableCellDataMap={tableCellDataMap}
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
    <Box sx={{ position: "absolute", top: 2, right: 2, padding: 2 }}>
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

function getNodeGroups(conns: Conns, sourceSet: Set<string>): NodeGroup[] {
  const groups: Record<string, NodeGroup> = {};
  for (const connKey in conns) {
    const connEntry = conns[connKey];

    if (connEntry.node_name && !sourceSet.has(connEntry.node_name)) {
      continue;
    }

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

function trimPrefix(fullNodeName: string): string {
  return fullNodeName.replace(/^[a-zA-Z0-9._-]+\//, "");
}

function ShowMoreDetails(props: {
  sample: PingSample;
  historySamples: PingSample[];
}) {
  const { sample, historySamples } = props;
  const lat = sample.peerExactLocation?.Latitude;
  const lon = sample.peerExactLocation?.Longitude;
  return (
    <Box>
      <Box>
        {sample.peer && <Box>Peer: {sample.peer}</Box>}
        {sample.peerRdns && <Box>RDNS: {sample.peerRdns}</Box>}
        {sample.peerASN && <Box>ASN: {sample.peerASN}</Box>}
        {sample.peerISP && <Box>ISP: {sample.peerISP}</Box>}
        {sample.peerLocation && <Box>Location: {sample.peerLocation}</Box>}
        {lat !== undefined &&
          lat !== null &&
          lon !== undefined &&
          lon !== null && (
            <Box>
              LatLon: {lat}, {lon}
            </Box>
          )}
      </Box>
      <Divider sx={{ margin: 1 }} orientation="horizontal"></Divider>
      <Box
        sx={{
          marginTop: 1,
          paddingTop: 1,
          maxHeight: "200px",
          overflow: "auto",
          display: "flex",
          flexDirection: "column-reverse",
        }}
      >
        {historySamples.map((sample, idx) => (
          <RenderPingSampleToText key={idx} sample={sample} />
        ))}
      </Box>
    </Box>
  );
}

function RenderPingSampleToText(props: {
  sample: PingSample | undefined | null;
}) {
  const { sample } = props;
  if (sample === undefined || sample === null) {
    return <Fragment></Fragment>;
  }
  if (sample.isTimeout) {
    const segs = ["Timeout:"];
    if (sample.seq !== undefined && sample.seq !== null) {
      segs.push(`seq=${sample.seq}`);
    }
    if (sample.ttl !== undefined && sample.ttl !== null) {
      segs.push(`ttl=${sample.ttl}`);
    }
    return <Fragment>{segs.join(" ")}</Fragment>;
  }
  const segs: string[] = [];
  if (sample.receivedSize !== undefined && sample.receivedSize !== null) {
    segs.push(`${sample.receivedSize} bytes`);
  }
  if (sample.peer) {
    if (sample.peerRdns) {
      segs.push(`from ${sample.peerRdns} (${sample.peer}):`);
    } else {
      segs.push(`from ${sample.peer}:`);
    }
  }
  if (sample.seq !== undefined && sample.seq !== null) {
    segs.push(`seq=${sample.seq}`);
  }
  if (sample.receivedTTL !== undefined && sample.receivedTTL !== null) {
    segs.push(`ttl=${sample.receivedTTL}`);
  }
  if (sample.mss !== undefined && sample.mss !== null) {
    segs.push(`mss=${sample.mss}`);
  }
  if (sample.latencyMs !== undefined && sample.latencyMs !== null) {
    segs.push(
      `time=${sample.latencyMs
        .toFixed(3)
        .replace(/0+$/, "")
        .replace(/\.$/, "")}ms`
    );
  }

  return <Box>{segs.join(" ")}</Box>;
}

const CustomWidthTooltip = styled(({ className, ...props }: TooltipProps) => (
  <Tooltip {...props} classes={{ popper: className }} />
))({
  [`& .${tooltipClasses.tooltip}`]: {
    maxWidth: 700,
  },
});

function RowMap(props: {
  target: string;
  sources: string[];
  rowLength: number;
  tableCellDataMap: TableCellDataMap;
}) {
  const { target, sources, rowLength, tableCellDataMap } = props;
  const [expanded, setExpanded] = useState<boolean>(false);

  const canvasX = 360000;
  const canvasY = 200000;

  const { canvasSvgRef } = useCanvasSizing(canvasX, canvasY, expanded, false);

  const { data: conns } = useQuery({
    queryKey: ["nodes"],
    queryFn: () => getNodes(),
  });

  const sourceSet = useMemo(() => {
    return new Set(sources ?? []);
  }, [sources]);

  let nodeGroups: NodeGroup[] = [];
  if (conns !== undefined && conns !== null) {
    nodeGroups = getNodeGroups(conns, sourceSet);
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
      const { latency } = getLatestDataFromMap(
        tableCellDataMap,
        target,
        node.node_name
      );
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
      index: nodeGroup.nodes?.[0]?.node_name
        ? trimPrefix(nodeGroup.nodes?.[0]?.node_name)
        : undefined,
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
          const {
            latency,
            mss,
            datum: sample,
            historySamples,
          } = getLatestDataFromMap(tableCellDataMap, target, source);

          return (
            <TableCell
              key={source}
              sx={{
                fontWeight: 500,
                minWidth: 100,
              }}
            >
              <Box>
                <CustomWidthTooltip
                  title={
                    sample ? (
                      <ShowMoreDetails
                        sample={sample}
                        historySamples={historySamples}
                      />
                    ) : (
                      <Fragment></Fragment>
                    )
                  }
                >
                  <Box
                    component="span"
                    sx={{ color: getLatencyColor(latency) }}
                  >
                    {!sample?.isTimeout &&
                    latency !== null &&
                    latency !== undefined
                      ? `${latency.toFixed(3)} ms`
                      : "—"}
                  </Box>
                </CustomWidthTooltip>
              </Box>
              {mss !== null && mss !== undefined && <Box>MSS={mss}</Box>}
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
                height: "360px",
                position: "relative",
                top: 0,
                left: 0,
              }}
            >
              <WorldMap
                canvasSvgRef={canvasSvgRef as any}
                canvasWidth={canvasX}
                canvasHeight={canvasY}
                fill="lightblue"
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
