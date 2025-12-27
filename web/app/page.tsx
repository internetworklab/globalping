"use client";

import {
  Box,
  Card,
  Typography,
  CardContent,
  TextField,
  Button,
  FormControlLabel,
  RadioGroup,
  Radio,
  Tooltip,
  IconButton,
  Checkbox,
  FormGroup,
} from "@mui/material";
import { CSSProperties, Fragment, useState } from "react";
import { SourcesSelector } from "@/components/sourceselector";
import { getCurrentPingers } from "@/apis/globalping";
import { PendingTask } from "@/apis/types";
import { TaskConfirmDialog } from "@/components/taskconfirm";
import { PingResultDisplay } from "@/components/pingdisplay";
import { TracerouteResultDisplay } from "@/components/traceroutedisplay";
import GitHubIcon from "@mui/icons-material/GitHub";

const fakeSources = [
  "192.168.1.1",
  "192.168.1.2",
  "192.168.1.3",
  "192.168.1.4",
  "192.168.1.5",
  "192.168.1.6",
  "192.168.1.7",
];

function getNextId(onGoingTasks: PendingTask[]): number {
  let maxId = 0;
  if (onGoingTasks.length > 0) {
    for (const task of onGoingTasks) {
      if (task.taskId > maxId) {
        maxId = task.taskId;
      }
    }
    maxId = maxId + 1;
  }
  return maxId;
}

function getSortedOnGoingTasks(onGoingTasks: PendingTask[]): PendingTask[] {
  const sortedTasks = [...onGoingTasks];
  sortedTasks.sort((a, b) => {
    return b.taskId - a.taskId;
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
      taskId: 0,
      type: "ping",
    };
  });
  const [openTaskConfirmDialog, setOpenTaskConfirmDialog] =
    useState<boolean>(false);

  const [sourcesInput, setSourcesInput] = useState<string>("");
  const [targetsInput, setTargetsInput] = useState<string>("");

  const [onGoingTasks, setOnGoingTasks] = useState<PendingTask[]>([
    // {
    //   sources: ["vie1"],
    //   targets: ["map.dn42"],
    //   taskId: 1,
    //   type: "traceroute",
    // },
  ]);

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

  const [taskType, setTaskType] = useState<"ping" | "traceroute">("ping");

  const repoAddr = process.env["NEXT_PUBLIC_GITHUB_REPO"];

  return (
    <Box sx={containerStyles}>
      <Box sx={headerCardStyles}>
        <Card>
          <CardContent>
            <Box
              sx={{
                display: "flex",
                justifyContent: "space-between",
                flexWrap: "wrap",
                rowGap: 2,
              }}
            >
              <Box
                sx={{
                  display: "flex",
                  alignItems: "center",
                  gap: 2,
                  flexWrap: "wrap",
                }}
              >
                <Typography variant="h6">GlobalPing</Typography>
                <RadioGroup
                  value={taskType}
                  onChange={(e) =>
                    setTaskType(e.target.value as "ping" | "traceroute")
                  }
                  row
                >
                  <FormControlLabel
                    value="ping"
                    control={<Radio />}
                    label="Ping"
                  />
                  <FormControlLabel
                    value="traceroute"
                    control={<Radio />}
                    label="Traceroute"
                  />
                </RadioGroup>
                <FormGroup row>
                  <FormControlLabel
                    control={
                      <Checkbox
                        checked={!!pendingTask.preferV4}
                        onChange={(_, ckd) => {
                          setPendingTask((prev) => ({
                            ...prev,
                            preferV4: !!ckd,
                            preferV6: ckd ? false : prev.preferV6,
                          }));
                        }}
                      />
                    }
                    label="Prefer V4"
                  />
                  <FormControlLabel
                    control={
                      <Checkbox
                        checked={!!pendingTask.preferV6}
                        onChange={(_, ckd) => {
                          setPendingTask((prev) => ({
                            ...prev,
                            preferV4: ckd ? false : prev.preferV4,
                            preferV6: !!ckd,
                          }));
                        }}
                      />
                    }
                    label="Prefer V6"
                  />
                </FormGroup>
              </Box>
              <Box sx={{ display: "flex", alignItems: "center", gap: 2 }}>
                {repoAddr !== "" && (
                  <Tooltip title="Go to Github Page">
                    <IconButton
                      onClick={() => {
                        window.open(repoAddr, "_blank");
                      }}
                    >
                      <GitHubIcon />
                    </IconButton>
                  </Tooltip>
                )}

                <Button
                  onClick={() => {
                    const srcs = dedup(sourcesInput.split(","))
                      .map((s) => s.trim())
                      .filter((s) => s.length > 0);
                    const tgts = dedup(targetsInput.split(","))
                      .map((t) => t.trim())
                      .filter((t) => t.length > 0);

                    setPendingTask((prev) => ({
                      ...prev,
                      sources: srcs,
                      targets: tgts,
                      taskId: getNextId(onGoingTasks),
                      type: taskType,
                    }));
                    setOpenTaskConfirmDialog(true);
                  }}
                >
                  Add Task
                </Button>
              </Box>
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
                placeholder={
                  taskType === "ping"
                    ? "Targets, separated by comma"
                    : "Specify a single target"
                }
                fullWidth
                label={taskType === "ping" ? "Targets" : "Target"}
                value={targetsInput}
                onChange={(e) => setTargetsInput(e.target.value)}
              />
            </Box>
          </CardContent>
        </Card>
        {getSortedOnGoingTasks(onGoingTasks).map((task) => (
          <Fragment key={task.taskId}>
            {task.type === "ping" ? (
              <PingResultDisplay
                pendingTask={task}
                onDeleted={() => {
                  setOnGoingTasks(
                    onGoingTasks.filter((t) => t.taskId !== task.taskId)
                  );
                }}
              />
            ) : (
              <TracerouteResultDisplay
                task={task}
                onDeleted={() => {
                  setOnGoingTasks(
                    onGoingTasks.filter((t) => t.taskId !== task.taskId)
                  );
                }}
              />
            )}
          </Fragment>
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
          setTargetsInput("");
        }}
      />
    </Box>
  );
}
