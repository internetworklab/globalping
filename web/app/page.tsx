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
  Link,
  Select,
  MenuItem,
  InputLabel,
  FormControl,
  FormLabel,
} from "@mui/material";
import { CSSProperties, Fragment, useState } from "react";
import { SourcesSelector } from "@/components/sourceselector";
import { getCurrentPingers } from "@/apis/globalping";
import { PendingTask } from "@/apis/types";
import { TaskConfirmDialog } from "@/components/taskconfirm";
import { PingResultDisplay } from "@/components/pingdisplay";
import { TracerouteResultDisplay } from "@/components/traceroutedisplay";
import GitHubIcon from "@mui/icons-material/GitHub";
import MoreHorizIcon from "@mui/icons-material/MoreHoriz";
import { About } from "@/components/about";

const fakeSources = [
  "192.168.1.1",
  "192.168.1.2",
  "192.168.1.3",
  "192.168.1.4",
  "192.168.1.5",
  "192.168.1.6",
  "192.168.1.7",
];

type DNSProbePlan = {
  transport: "udp" | "tcp";
  type: "a" | "aaaa" | "cname" | "mx" | "ns" | "ptr" | "txt";
  domains: string[];
  resolvers: string[];
};

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
      type: "dns",
    };
  });
  const [openTaskConfirmDialog, setOpenTaskConfirmDialog] =
    useState<boolean>(false);

  const [sourcesInput, setSourcesInput] = useState<string>("");
  const [targetsInput, setTargetsInput] = useState<string>("");

  const [dnsProbePlan, setDnsProbePlan] = useState<DNSProbePlan>({
    transport: "udp",
    type: "a",
    domains: [],
    resolvers: [],
  });
  console.log("[dbg] dns probe plan", dnsProbePlan);

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

  const repoAddr = process.env["NEXT_PUBLIC_GITHUB_REPO"];
  const dn42GeoIPRepo = process.env["NEXT_PUBLIC_DN42_GEOIP_REPO"];
  const [showAboutDialog, setShowAboutDialog] = useState<boolean>(false);

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
                gap: 2,
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
                <Typography variant="h6">MyGlobalping</Typography>
              </Box>
              <Box sx={{ display: "flex", alignItems: "center", gap: 2 }}>
                {!!dn42GeoIPRepo && (
                  <Tooltip title="Go visit DN42 GeoIP Project">
                    <Link
                      underline="hover"
                      href={dn42GeoIPRepo}
                      target="_blank"
                      variant="caption"
                      sx={{ display: "flex", alignItems: "center", gap: 1 }}
                    >
                      DN42GeoIP
                      <GitHubIcon />
                    </Link>
                  </Tooltip>
                )}
                {repoAddr !== "" && (
                  <Tooltip title="Go to Project's Github Page">
                    <Link
                      underline="hover"
                      href={repoAddr}
                      target="_blank"
                      variant="caption"
                      sx={{ display: "flex", alignItems: "center", gap: 1 }}
                    >
                      Project
                      <GitHubIcon />
                    </Link>
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
                    }));
                    setOpenTaskConfirmDialog(true);
                  }}
                >
                  Add Task
                </Button>
              </Box>
            </Box>
            <Box
              sx={{
                marginTop: 2,
                display: "flex",
                justifyContent: "space-between",
                alignItems: "center",
                flexWrap: "wrap",
                gap: 2,
              }}
            >
              <Box
                sx={{
                  display: "flex",
                  gap: 2,
                  flexWrap: "wrap",
                  alignItems: "center",
                }}
              >
                <FormControl>
                  <FormLabel>Task Type</FormLabel>
                  <RadioGroup
                    value={pendingTask.type}
                    onChange={(e) =>
                      setPendingTask((prev) => ({
                        ...prev,
                        type: e.target.value as "ping" | "traceroute",
                        pmtu:
                          e.target.value === "ping" ||
                          e.target.value === "tcpping"
                            ? false
                            : prev.pmtu,
                        useUDP:
                          e.target.value === "tcpping" ? false : prev.useUDP,
                      }))
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
                    <FormControlLabel
                      value="tcpping"
                      control={<Radio />}
                      label="TCP Ping"
                    />
                    <FormControlLabel
                      value="dns"
                      control={<Radio />}
                      label="DNS"
                    />
                  </RadioGroup>
                </FormControl>
              </Box>
              <Tooltip title="more">
                <IconButton
                  size="small"
                  onClick={() => setShowAboutDialog(true)}
                >
                  <MoreHorizIcon fontSize="small" />
                </IconButton>
              </Tooltip>
            </Box>
            <Box sx={{ marginTop: 2 }}>
              {pendingTask.type === "dns" ? (
                <FormControl>
                  <FormLabel>Options</FormLabel>
                  <RadioGroup
                    row
                    value={dnsProbePlan.transport}
                    onChange={(e) =>
                      setDnsProbePlan((prev) => ({
                        ...prev,
                        transport: e.target.value as "udp" | "tcp",
                      }))
                    }
                  >
                    <FormControlLabel
                      control={<Radio />}
                      value="udp"
                      label="Use UDP"
                    />
                    <FormControlLabel
                      control={<Radio />}
                      value="tcp"
                      label="Use TCP"
                    />
                  </RadioGroup>
                </FormControl>
              ) : (
                <FormControl>
                  <FormLabel>Options</FormLabel>
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
                    <FormControlLabel
                      control={
                        <Checkbox
                          disabled={pendingTask.type === "tcpping"}
                          checked={!!pendingTask.useUDP}
                          onChange={(_, ckd) => {
                            setPendingTask((prev) => ({
                              ...prev,
                              useUDP: !!ckd,
                            }));
                          }}
                        />
                      }
                      label="Use UDP"
                    />
                    <FormControlLabel
                      control={
                        <Checkbox
                          disabled={pendingTask.type !== "traceroute"}
                          checked={
                            pendingTask.type === "traceroute" &&
                            !!pendingTask.pmtu
                          }
                          onChange={(_, ckd) => {
                            setPendingTask((prev) => ({
                              ...prev,
                              pmtu: !!ckd,
                            }));
                          }}
                        />
                      }
                      label="PMTU"
                    />
                  </FormGroup>
                </FormControl>
              )}
            </Box>
            <Box sx={{ marginTop: 2 }}>
              <SourcesSelector
                value={sourcesInput
                  .split(",")
                  .map((s) => s.trim())
                  .filter((s) => s.length > 0)}
                onChange={(value) => setSourcesInput(value.join(","))}
                getOptions={() => {
                  let filter: Record<string, string> | undefined = undefined;
                  if (!!pendingTask.useUDP) {
                    filter = { ...(filter || {}), SupportUDP: "true" };
                  }
                  if (!!pendingTask.pmtu) {
                    filter = { ...(filter || {}), SupportPMTU: "true" };
                  }
                  if (pendingTask.type === "tcpping") {
                    filter = { ...(filter || {}), SupportTCP: "true" };
                  }
                  if (pendingTask.type === "dns") {
                    filter = { ...(filter || {}), CapabilityDNSProbe: "true" };
                  }

                  return getCurrentPingers(filter);
                  // return new Promise((res) => {
                  //   window.setTimeout(() => res(fakeSources), 2000);
                  // });
                }}
              />
            </Box>
            <Box sx={{ marginTop: 2 }}>
              {pendingTask.type === "dns" ? (
                <Box>
                  <FormControl fullWidth variant="standard">
                    <InputLabel>Type</InputLabel>
                    <Select label="Type">
                      <MenuItem value={"a"}>A</MenuItem>
                      <MenuItem value={"aaaa"}>AAAA</MenuItem>
                      <MenuItem value={"cname"}>CNAME</MenuItem>
                      <MenuItem value={"mx"}>MX</MenuItem>
                      <MenuItem value={"ns"}>NS</MenuItem>
                      <MenuItem value={"ptr"}>PTR</MenuItem>
                      <MenuItem value={"txt"}>TXT</MenuItem>
                    </Select>
                  </FormControl>
                  <TextField
                    sx={{ marginTop: 2 }}
                    variant="standard"
                    placeholder="Querying Domains, separated by comma"
                    fullWidth
                    label="Domains"
                  />
                  <TextField
                    sx={{ marginTop: 2 }}
                    variant="standard"
                    placeholder="Servers where to send queries, separated by comma, e.g. 8.8.8.8, or [2001:4860:4860::8888]:53"
                    fullWidth
                    label="Resolvers"
                  />
                </Box>
              ) : (
                <TextField
                  variant="standard"
                  placeholder={
                    pendingTask.type === "ping"
                      ? "Targets, separated by comma"
                      : pendingTask.type === "tcpping"
                      ? 'Specify a single target, in the format of <host>:<port>", e.g. 192.168.1.1:80, or cloudflare.com:443'
                      : "Specify a single target"
                  }
                  fullWidth
                  label={pendingTask.type === "ping" ? "Targets" : "Target"}
                  value={targetsInput}
                  onChange={(e) => setTargetsInput(e.target.value)}
                />
              )}
            </Box>
          </CardContent>
        </Card>
        {getSortedOnGoingTasks(onGoingTasks).map((task) => (
          <Fragment key={task.taskId}>
            {task.type === "traceroute" ? (
              <TracerouteResultDisplay
                task={task}
                onDeleted={() => {
                  setOnGoingTasks(
                    onGoingTasks.filter((t) => t.taskId !== task.taskId)
                  );
                }}
              />
            ) : (
              <PingResultDisplay
                pendingTask={task}
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
      <Box sx={{ height: "100vh" }}></Box>
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
      <About open={showAboutDialog} onClose={() => setShowAboutDialog(false)} />
    </Box>
  );
}
