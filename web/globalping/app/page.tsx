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
  Button,
  Dialog,
  DialogTitle,
  DialogContent,
  DialogActions,
  IconButton,
  Tooltip,
  TableContainer,
} from "@mui/material";
import { CSSProperties, Fragment, useEffect, useRef, useState } from "react";
import CloseIcon from "@mui/icons-material/CloseOutlined";
import { SourcesSelector } from "@/components/sourceselector";
import {
  getCurrentPingers,
  PingSample,
  generateFakePingSampleStream,
  generatePingSampleStream,
} from "@/apis/globalping";
import PauseIcon from "@mui/icons-material/Pause";
import PlayArrowIcon from "@mui/icons-material/PlayArrow";

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

function PingResultDisplay(props: {
  pendingTask: PendingTask;
  onDeleted: () => void;
}) {
  const { pendingTask, onDeleted } = props;
  const { sources, targets } = pendingTask;

  const [latencyMap, setLatencyMap] = useState<
    Record<string, Record<string, number>>
  >({});
  const getLatency = (
    source: string,
    target: string
  ): number | undefined | null => {
    return latencyMap[target]?.[source];
  };

  const getLatencyColor = (latency: number | null | undefined): string => {
    if (latency === null || latency === undefined) {
      return "inherit"; // Default color for missing data
    }
    if (latency <= 40) {
      return "#4caf50"; // Green for [0-40]ms
    } else if (latency <= 150) {
      return "#ff9800"; // Yellow for (40-150]ms
    } else {
      return "#f44336"; // Red for (150, +inf)ms
    }
  };

  const [running, setRunning] = useState<boolean>(true);

  function launchStream(): [
    ReadableStream<PingSample>,
    ReadableStreamDefaultReader<PingSample>
  ] {
    // const resultStream = generateFakePingSampleStream(sources, targets);
    const resultStream = generatePingSampleStream({
      sources: sources,
      targets: targets,
      count: 1000,
      intervalMs: 100,
      pktTimeoutMs: 3000,
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

        setLatencyMap((prev) => ({
          ...prev,
          [sampleTarget]: {
            ...(prev[sampleTarget] || {}),
            [sampleFrom]: sampleLatency,
          },
        }));
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
    if (running) {
      const [_, reader] = launchStream();
      readerRef.current = reader;
    }

    return () => {
      cancelStream();
    };
  }, [pendingTask.taskId, running]);

  return (
    <Fragment>
      <Box
        sx={{
          display: "flex",
          justifyContent: "space-between",
          alignItems: "center",
        }}
      >
        <Typography variant="h6">Task #{pendingTask.taskId}</Typography>
        <Box sx={{ display: "flex", gap: 1, alignItems: "center" }}>
          <Tooltip title={running ? "Running" : "Stopped"}>
            <IconButton
              onClick={() => {
                if (running) {
                  cancelStream();
                  setRunning(false);
                } else {
                  setRunning(true);
                }
              }}
            >
              {running ? <PauseIcon /> : <PlayArrowIcon />}
            </IconButton>
          </Tooltip>
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
            </TableRow>
          </TableHead>
          <TableBody>
            {targets.map((target) => (
              <TableRow key={target}>
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
              </TableRow>
            ))}
          </TableBody>
        </Table>
      </TableContainer>
    </Fragment>
  );
}

function TaskCloseIconButton(props: {
  onConfirmedClosed: () => void;
  taskId: string;
}) {
  const { onConfirmedClosed, taskId } = props;
  const [showDialog, setShowDialog] = useState<boolean>(false);
  return (
    <Fragment>
      <Tooltip title="Close Task">
        <IconButton
          onClick={() => {
            setShowDialog(true);
          }}
        >
          <CloseIcon />
        </IconButton>
      </Tooltip>
      <Dialog open={showDialog} onClose={() => setShowDialog(false)}>
        <DialogTitle>Close Task</DialogTitle>
        <DialogContent>
          <Typography>
            Are you sure you want to close task #{taskId}?
          </Typography>
        </DialogContent>
        <DialogActions>
          <Button onClick={() => setShowDialog(false)}>Cancel</Button>
          <Button onClick={onConfirmedClosed}>Close</Button>
        </DialogActions>
      </Dialog>
    </Fragment>
  );
}

type PendingTask = {
  sources: string[];
  targets: string[];
  taskId: string;
};
function TaskConfirmDialog(props: {
  pendingTask: PendingTask;
  open: boolean;
  onCancel: () => void;
  onConfirm: () => void;
}) {
  const { open, pendingTask, onConfirm, onCancel } = props;

  if (pendingTask.sources.length === 0 || pendingTask.targets.length === 0) {
    return (
      <Dialog maxWidth="sm" fullWidth open={open} onClose={onCancel}>
        <DialogTitle>Confirm Task</DialogTitle>
        <DialogContent>
          At least one source and one target are required.
        </DialogContent>
        <DialogActions>
          <Button onClick={onCancel}>Confirm</Button>
        </DialogActions>
      </Dialog>
    );
  }

  return (
    <Fragment>
      <Dialog maxWidth="sm" fullWidth open={open} onClose={onCancel}>
        <DialogTitle>Confirm Task</DialogTitle>
        <DialogContent>
          <Typography gutterBottom>
            Sources: {pendingTask.sources.join(", ")}
          </Typography>
          <Typography>Targets: {pendingTask.targets.join(", ")}</Typography>
        </DialogContent>
        <DialogActions>
          <Button onClick={onCancel}>Cancel</Button>
          <Button onClick={onConfirm}>Confirm</Button>
        </DialogActions>
      </Dialog>
    </Fragment>
  );
}

function getSortedOnGoingTasks(onGoingTasks: PendingTask[]): PendingTask[] {
  const sortedTasks = [...onGoingTasks];
  sortedTasks.sort((a, b) => {
    return b.taskId.localeCompare(a.taskId);
  });
  return sortedTasks;
}

function dedup(arr: string[]): string[] {
  return Array.from(new Set(arr));
}

export default function Home() {
  const [pendingTask, setPendingTask] = useState<PendingTask>(() => {
    return {
      sources: [],
      targets: [],
      taskId: "",
    };
  });
  const [openTaskConfirmDialog, setOpenTaskConfirmDialog] =
    useState<boolean>(false);

  const [sourcesInput, setSourcesInput] = useState<string>("");
  const [targetsInput, setTargetsInput] = useState<string>("");

  const [onGoingTasks, setOnGoingTasks] = useState<PendingTask[]>([]);

  let containerStyles: CSSProperties[] = [
    {
      position: "relative",
      left: 0,
      top: 0,
      height: "100vh",
      width: "100vw",
      overflow: "auto",
    },
  ];

  let headerCardStyles: CSSProperties[] = [
    { padding: 2, display: "flex", flexDirection: "column", gap: 2 },
  ];

  if (onGoingTasks.length === 0) {
    containerStyles = [
      ...containerStyles,
      { display: "flex", justifyContent: "center", alignItems: "center" },
    ];
    headerCardStyles = [...headerCardStyles, { width: "80%" }];
  }

  return (
    <Box sx={containerStyles}>
      <Box sx={headerCardStyles}>
        <Card>
          <CardContent>
            <Box sx={{ display: "flex", justifyContent: "space-between" }}>
              <Typography variant="h6">GlobalPing</Typography>
              <Button
                onClick={() => {
                  const srcs = dedup(sourcesInput.split(","))
                    .map((s) => s.trim())
                    .filter((s) => s.length > 0);
                  const tgts = dedup(targetsInput.split(","))
                    .map((t) => t.trim())
                    .filter((t) => t.length > 0);
                  setPendingTask({
                    sources: srcs,
                    targets: tgts,
                    taskId: onGoingTasks.length.toString(),
                  });
                  setOpenTaskConfirmDialog(true);
                }}
              >
                Add Task
              </Button>
            </Box>
            <Box sx={{ marginTop: 2 }}>
              <SourcesSelector
                value={sourcesInput
                  .split(",")
                  .map((s) => s.trim())
                  .filter((s) => s.length > 0)}
                onChange={(value) => setSourcesInput(value.join(","))}
                getOptions={() => {
                  return getCurrentPingers();
                  // return new Promise((res) => {
                  //   window.setTimeout(() => res(fakeSources), 2000);
                  // });
                }}
              />
            </Box>
            <Box sx={{ marginTop: 2 }}>
              <TextField
                variant="standard"
                placeholder="Targets, separated by comma"
                fullWidth
                label="Targets"
                value={targetsInput}
                onChange={(e) => setTargetsInput(e.target.value)}
              />
            </Box>
          </CardContent>
        </Card>
        {getSortedOnGoingTasks(onGoingTasks).map((task) => (
          <Card key={task.taskId}>
            <CardContent>
              <PingResultDisplay
                pendingTask={task}
                onDeleted={() => {
                  setOnGoingTasks(
                    onGoingTasks.filter((t) => t.taskId !== task.taskId)
                  );
                }}
              />
            </CardContent>
          </Card>
        ))}
      </Box>
      <TaskConfirmDialog
        pendingTask={pendingTask}
        open={openTaskConfirmDialog}
        onCancel={() => {
          setOpenTaskConfirmDialog(false);
        }}
        onConfirm={() => {
          setOnGoingTasks([...onGoingTasks, pendingTask]);
          setOpenTaskConfirmDialog(false);
          setSourcesInput("");
          setTargetsInput("");
        }}
      />
    </Box>
  );
}
