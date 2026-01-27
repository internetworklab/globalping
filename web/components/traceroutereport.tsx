"use client";

import {
  Box,
  Button,
  Dialog,
  DialogContent,
  DialogTitle,
  IconButton,
  Tooltip,
} from "@mui/material";
import { useMemo, useRef, useState } from "react";
import SaveIcon from "@mui/icons-material/Save";
import { Row, CanvasTable } from "@/components/canvastable";

export type TracerouteReportLocation = {
  city?: string;
  countryAlpha2?: string;
};

function formatNumStr(num: string): string {
  return num.replace(/0+$/, "").replace(/\.$/, "");
}

function getNumStats(arr: number[]):
  | {
      min: number;
      max: number;
      med: number;
    }
  | undefined {
  if (arr.length > 0) {
    const sorted = [...arr];
    sorted.sort();

    let med = 0;
    if (arr.length % 2) {
      med = sorted[Math.floor(arr.length / 2)];
    } else {
      med += sorted[arr.length / 2 - 1];
      med += sorted[arr.length / 2];
      med = med / 2;
    }

    return { min: sorted[0], max: sorted[sorted.length - 1], med };
  }
  return undefined;
}

function renderLoc(loc?: TracerouteReportLocation): string {
  if (!loc) {
    return "";
  }
  const locLine: string[] = [];
  if (loc.city) {
    locLine.push(loc.city);
  }
  if (loc.countryAlpha2) {
    locLine.push(loc.countryAlpha2);
  }
  if (locLine.length === 0) {
    return "";
  }
  return locLine.join(", ");
}

export type TracerouteReportISP = {
  // name of the isp, like 'Hurricane Electric'
  ispName: string;

  // usually in the format like 'AS12345'
  asn: string;
};

function renderISP(isp?: TracerouteReportISP): string {
  if (!isp) {
    return "";
  }
  const ispLine: string[] = [];
  if (isp.asn) {
    ispLine.push(isp.asn);
  }
  if (isp.ispName) {
    ispLine.push(isp.ispName);
  }
  if (ispLine.length === 0) {
    return "";
  }
  return ispLine.join(" ");
}

export type TracerouteReportSource = {
  nodeName: string;
  isp?: TracerouteReportISP;
  loc?: TracerouteReportLocation;
};

function renderSource(src?: TracerouteReportSource): string {
  if (!src) {
    return "";
  }

  if (!src.nodeName) {
    return "";
  }

  const srcLine: string[] = [];
  srcLine.push("Node " + src.nodeName);
  const ispLine = renderISP(src.isp);
  if (ispLine) {
    srcLine.push(ispLine);
  }
  const locLine = renderLoc(src.loc);
  if (locLine) {
    srcLine.push(locLine);
  }
  return srcLine.join(", ");
}

export type TracerouteReportMode = "tcp" | "icmp" | "udp";

export type TracerouteReportRTTStat = {
  lastMs: number;
  samples: number[];
};

function renderRTTStat(stat?: TracerouteReportRTTStat): string {
  if (!stat) {
    return "";
  }
  const stats = getNumStats(stat.samples);
  let rttStr = `${formatNumStr(stat.lastMs.toFixed(2))}ms`;
  if (stats) {
    const statsLine = [
      `${formatNumStr(stats.min.toFixed(2))}ms`,
      `${formatNumStr(stats.med.toFixed(2))}ms`,
      `${formatNumStr(stats.max.toFixed(2))}ms`,
    ];
    rttStr += ` ${statsLine.join("/")}`;
  }
  return rttStr;
}

export type TracerouteReportTXRXStat = {
  sent: number;
  replies: number;
};

function renderTXRXStat(stat?: TracerouteReportTXRXStat): string {
  if (!stat) {
    return "";
  }
  const lossPercent =
    stat.sent > 0 ? ((stat.sent - stat.replies) / stat.sent) * 100 : 0;
  return [
    `${stat.sent} sent`,
    `${stat.replies} replies`,
    `${formatNumStr(lossPercent.toFixed(2))}% loss`,
  ].join(", ");
}

export type TracerouteReportPeer = {
  // if this field is falsy, mark it with a '*' in the screen,
  // and skip render all the following fields.
  timeout?: boolean;
  rdns?: string;
  ip: string;
  loc?: TracerouteReportLocation;
  isp?: TracerouteReportISP;
  rtt?: TracerouteReportRTTStat;
  stat?: TracerouteReportTXRXStat;
  pmtu?: number;
};

export type TracerouteReportHop = {
  // ttl of the sent packets, usually, when doing traceroute, start with ttl=1,
  // then increment ttl one by one until the final target is reached.
  ttl: number;

  // if the middlebox router is doing something like ecmp, a hop could be various peers.
  peers: TracerouteReportPeer[];
};

export type TracerouteReport = {
  // when the report is generated
  date: number;

  // in case that a originating node is multi-homed/BGP
  sources: TracerouteReportSource[];

  // the domain or ip address of the target host
  destination: string;

  // type of l4 sending packets, for linux, traceroute use udp by default,
  // for windows, icmp is used.
  mode: TracerouteReportMode;

  hops: TracerouteReportHop[];
};

function renderTracerouteReport(report: TracerouteReport): {
  preamble: Row[];
  tabularData: Row[];
} {
  const preamble: Row[] = [];

  if (report.date !== 0) {
    preamble.push([
      { content: `Date: ${new Date(report.date).toISOString()}` },
    ]);
  }
  for (const src of report.sources) {
    const line = renderSource(src);
    if (line) {
      preamble.push([{ content: "Source: " + line }]);
    }
  }

  if (report.destination) {
    preamble.push([{ content: "Destination: " + report.destination }]);
  }

  if (report.mode) {
    preamble.push([{ content: "Mode: " + report.mode.toUpperCase() }]);
  }

  const tabularData: Row[] = [];
  if (report.hops && report.hops.length > 0) {
    const header: Row = [
      { content: "TTL" },
      { content: "Peers" },
      { content: "ISP" },
      { content: "Location" },
      { content: "RTTs (last min/med/max)" },
      { content: "Stat" },
    ];
    tabularData.push(header);

    for (const hop of report.hops) {
      for (let peerIdx in hop.peers) {
        const peer = hop.peers[peerIdx];
        const row: Row = [];

        // TTL
        if (peerIdx === "0") {
          row.push({ content: String(hop.ttl) });
        } else {
          row.push({ content: "" });
        }

        // Peers
        if (peer.timeout) {
          row.push({ content: "*" });
          for (let i = 1; i < header.length; i++) {
            row.push({ content: "" });
          }
          tabularData.push(row);
          continue;
        } else {
          let peerName = "";
          if (peer.rdns) {
            peerName = peer.rdns + " " + `(${peer.ip})`;
          } else {
            peerName = peer.ip;
          }
          if (peer.pmtu !== undefined && peer.pmtu !== null) {
            peerName += ` [PMTU=${peer.pmtu}]`;
          }
          row.push({ content: peerName });
        }

        // ISP
        row.push({ content: renderISP(peer.isp) });

        // Location
        row.push({ content: renderLoc(peer.loc) });

        // RTTs
        row.push({ content: renderRTTStat(peer.rtt) });

        // Stats
        row.push({ content: renderTXRXStat(peer.stat) });

        tabularData.push(row);
      }
    }
  }

  return { preamble, tabularData };
}

export type PingReport = {
  date: number;
  mode: TracerouteReportMode;
  sources: string[];
  targets: string[];

  preferV4?: boolean;
  preferV6?: boolean;

  // to -> from -> rtt
  rtts: Record<string, Record<string, number>>;
};

function renderPingReport(report: PingReport): {
  preamble: Row[];
  tabularData: Row[];
} {
  const preamble: Row[] = [];
  const tabularData: Row[] = [];

  if (report.date !== 0) {
    preamble.push([
      { content: `Date: ${new Date(report.date).toISOString()}` },
    ]);
  }

  if (report.mode) {
    preamble.push([{ content: "Mode: " + report.mode.toUpperCase() }]);
  }

  if (!!report.preferV4) {
    preamble.push([{ content: "Prefer IPv4: true" }]);
  }

  if (!!report.preferV6) {
    preamble.push([{ content: "Prefer IPv6: true" }]);
  }

  if (report.sources && report.sources.length > 0) {
    const header: Row = [{ content: "Tgt\\Src" }];
    for (const src of report.sources) {
      header.push({ content: src?.toUpperCase() || "" });
    }
    tabularData.push(header);

    if (report.targets && report.targets.length > 0) {
      for (const tgt of report.targets) {
        const tgtRow: Row = [{ content: tgt }];

        for (const src of report.sources) {
          const rtt = report.rtts?.[tgt]?.[src];
          if (rtt !== undefined && rtt !== null) {
            tgtRow.push({ content: `${formatNumStr(rtt.toFixed(2))}ms` });
          }
        }

        tabularData.push(tgtRow);
      }
    }
  }

  return { preamble, tabularData };
}

function exportCanvasBitmap(
  canvasRef: React.RefObject<HTMLCanvasElement | null>,
  fname: string
): Promise<void> {
  const canvasEle = canvasRef.current;
  if (!canvasEle) {
    console.error("no canvas element found.");
  }
  const mimeType = "image/png";
  fname = fname.replaceAll(" ", "_");

  return new Promise<void>((resolve) => {
    canvasEle!.toBlob((blob) => {
      if (!blob) {
        console.error("cant export canvas to blob");
      }

      const f = new File([blob!], fname, { type: mimeType });
      const url = URL.createObjectURL(f);
      const aEle = window.document.createElement("a");
      aEle.setAttribute("href", url);
      aEle.setAttribute("download", f.name);
      aEle.click();
      resolve();
    }, mimeType);
  });
}

export function TracerouteReportPreviewDialog(props: {
  report: TracerouteReport | undefined;
  open: boolean;
  onClose: () => void;
}) {
  const { report, open, onClose } = props;

  const canvasRef = useRef<HTMLCanvasElement>(null);

  const { preamble, tabularData } = useMemo(() => {
    if (report) {
      return renderTracerouteReport(report);
    }
    return { preamble: [], tabularData: [] };
  }, [report]);

  const [exporting, setExporting] = useState(false);

  return (
    <Dialog open={open} onClose={onClose} maxWidth={"md"} fullWidth>
      <DialogTitle>
        <Box
          sx={{
            display: "flex",
            justifyContent: "space-between",
            flexWrap: "wrap",
            gap: 2,
            alignItems: "center",
          }}
        >
          Preview
          <Tooltip title="Save to disk">
            <IconButton
              loading={exporting}
              onClick={() => {
                const date = report?.date;
                if (date !== undefined && date !== null) {
                  const fname = `trace-${new Date(date).toISOString()}.png`;
                  setExporting(true);
                  exportCanvasBitmap(canvasRef, fname).finally(() =>
                    setExporting(false)
                  );
                }
              }}
            >
              <SaveIcon />
            </IconButton>
          </Tooltip>
        </Box>
      </DialogTitle>
      <DialogContent sx={{ paddingLeft: 0, paddingRight: 0, paddingBottom: 0 }}>
        <CanvasTable
          preamble={preamble}
          tabularData={tabularData}
          canvasRef={canvasRef as any}
        />
      </DialogContent>
    </Dialog>
  );
}

export function PingReportPreviewDialog(props: {
  report: PingReport | undefined;
  open: boolean;
  onClose: () => void;
}) {
  const { report, open, onClose } = props;

  const canvasRef = useRef<HTMLCanvasElement>(null);

  const { preamble, tabularData } = useMemo(() => {
    if (report) {
      return renderPingReport(report);
    }
    return { preamble: [], tabularData: [] };
  }, [report]);

  const [exporting, setExporting] = useState(false);

  return (
    <Dialog open={open} onClose={onClose} maxWidth={"md"} fullWidth>
      <DialogTitle>
        <Box
          sx={{
            display: "flex",
            justifyContent: "space-between",
            flexWrap: "wrap",
            gap: 2,
            alignItems: "center",
          }}
        >
          Preview
          <Tooltip title="Save to disk">
            <IconButton
              loading={exporting}
              onClick={() => {
                const date = report?.date;
                if (date !== undefined && date !== null) {
                  const fname = `ping-${new Date(date).toISOString()}.png`;
                  setExporting(true);
                  exportCanvasBitmap(canvasRef, fname).finally(() =>
                    setExporting(false)
                  );
                }
              }}
            >
              <SaveIcon />
            </IconButton>
          </Tooltip>
        </Box>
      </DialogTitle>
      <DialogContent sx={{ paddingLeft: 0, paddingRight: 0, paddingBottom: 0 }}>
        <CanvasTable
          preamble={preamble}
          tabularData={tabularData}
          canvasRef={canvasRef as any}
        />
      </DialogContent>
    </Dialog>
  );
}
