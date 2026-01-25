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
  let rttStr = `${stat.lastMs}ms`;
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

export function renderTracerouteReport(report: TracerouteReport): {
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

export const LONG_TRACEROUTE_DATA: TracerouteReport = {
  date: 1737839945000, // Jan 25, 2026
  sources: [
    {
      nodeName: "TYO-EDGE-01",
      isp: { ispName: "NTT Communications", asn: "AS2914" },
      loc: { city: "Tokyo", countryAlpha2: "JP" },
    },
  ],
  destination: "api.global-services.uk",
  mode: "udp",
  hops: [
    // Local Network
    {
      ttl: 1,
      peers: [
        {
          ip: "192.168.1.1",
          rtt: { lastMs: 0.42, samples: [0.39, 0.45, 0.42, 0.41, 0.44] },
          stat: { sent: 5, replies: 5 },
        },
      ],
    },
    {
      ttl: 2,
      peers: [
        {
          ip: "10.0.44.1",
          rtt: { lastMs: 1.12, samples: [1.05, 1.21, 1.12, 1.08, 1.15] },
          stat: { sent: 5, replies: 5 },
        },
      ],
    },
    // ISP Aggregation
    {
      ttl: 3,
      peers: [
        {
          rdns: "tyo-n01.vrio.ntt.net",
          ip: "153.153.0.1",
          isp: { ispName: "NTT", asn: "AS2914" },
          loc: { city: "Tokyo", countryAlpha2: "JP" },
          rtt: { lastMs: 2.45, samples: [2.1, 3.2, 2.45, 2.3, 2.5] },
          stat: { sent: 5, replies: 5 },
        },
      ],
    },
    // ECMP - Load balancing across two routers
    {
      ttl: 4,
      peers: [
        {
          rdns: "ae-1.r01.tokyjp05.jp.bb.gin.ntt.net",
          ip: "129.250.6.121",
          isp: { ispName: "NTT", asn: "AS2914" },
          rtt: { lastMs: 3.12, samples: [3.0, 3.5, 3.12] },
          stat: { sent: 3, replies: 3 },
        },
        {
          rdns: "ae-2.r01.tokyjp05.jp.bb.gin.ntt.net",
          ip: "129.250.6.125",
          isp: { ispName: "NTT", asn: "AS2914" },
          rtt: { lastMs: 3.08, samples: [2.9, 3.2, 3.08] },
          stat: { sent: 2, replies: 2 },
        },
        {
          rdns: "ae-1.r01.tokyjp05.jp.bb.gin.ntt.net",
          ip: "129.250.6.121",
          isp: { ispName: "NTT", asn: "AS2914" },
          rtt: { lastMs: 3.12, samples: [3.0, 3.5, 3.12] },
          stat: { sent: 3, replies: 3 },
        },
        {
          rdns: "ae-2.r01.tokyjp05.jp.bb.gin.ntt.net",
          ip: "129.250.6.125",
          isp: { ispName: "NTT", asn: "AS2914" },
          rtt: { lastMs: 3.08, samples: [2.9, 3.2, 3.08] },
          stat: { sent: 2, replies: 2 },
        },
        {
          rdns: "ae-1.r01.tokyjp05.jp.bb.gin.ntt.net",
          ip: "129.250.6.121",
          isp: { ispName: "NTT", asn: "AS2914" },
          rtt: { lastMs: 3.12, samples: [3.0, 3.5, 3.12] },
          stat: { sent: 3, replies: 3 },
        },
        {
          rdns: "ae-2.r01.tokyjp05.jp.bb.gin.ntt.net",
          ip: "129.250.6.125",
          isp: { ispName: "NTT", asn: "AS2914" },
          rtt: { lastMs: 3.08, samples: [2.9, 3.2, 3.08] },
          stat: { sent: 2, replies: 2 },
        },
        {
          rdns: "ae-1.r01.tokyjp05.jp.bb.gin.ntt.net",
          ip: "129.250.6.121",
          isp: { ispName: "NTT", asn: "AS2914" },
          rtt: { lastMs: 3.12, samples: [3.0, 3.5, 3.12] },
          stat: { sent: 3, replies: 3 },
        },
        {
          rdns: "ae-2.r01.tokyjp05.jp.bb.gin.ntt.net",
          ip: "129.250.6.125",
          isp: { ispName: "NTT", asn: "AS2914" },
          rtt: { lastMs: 3.08, samples: [2.9, 3.2, 3.08] },
          stat: { sent: 2, replies: 2 },
        },
        {
          rdns: "ae-1.r01.tokyjp05.jp.bb.gin.ntt.net",
          ip: "129.250.6.121",
          isp: { ispName: "NTT", asn: "AS2914" },
          rtt: { lastMs: 3.12, samples: [3.0, 3.5, 3.12] },
          stat: { sent: 3, replies: 3 },
        },
        {
          rdns: "ae-2.r01.tokyjp05.jp.bb.gin.ntt.net",
          ip: "129.250.6.125",
          isp: { ispName: "NTT", asn: "AS2914" },
          rtt: { lastMs: 3.08, samples: [2.9, 3.2, 3.08] },
          stat: { sent: 2, replies: 2 },
        },
        {
          rdns: "ae-1.r01.tokyjp05.jp.bb.gin.ntt.net",
          ip: "129.250.6.121",
          isp: { ispName: "NTT", asn: "AS2914" },
          rtt: { lastMs: 3.12, samples: [3.0, 3.5, 3.12] },
          stat: { sent: 3, replies: 3 },
        },
        {
          rdns: "ae-2.r01.tokyjp05.jp.bb.gin.ntt.net",
          ip: "129.250.6.125",
          isp: { ispName: "NTT", asn: "AS2914" },
          rtt: { lastMs: 3.08, samples: [2.9, 3.2, 3.08] },
          stat: { sent: 2, replies: 2 },
        },
        {
          rdns: "ae-1.r01.tokyjp05.jp.bb.gin.ntt.net",
          ip: "129.250.6.121",
          isp: { ispName: "NTT", asn: "AS2914" },
          rtt: { lastMs: 3.12, samples: [3.0, 3.5, 3.12] },
          stat: { sent: 3, replies: 3 },
        },
        {
          rdns: "ae-2.r01.tokyjp05.jp.bb.gin.ntt.net",
          ip: "129.250.6.125",
          isp: { ispName: "NTT", asn: "AS2914" },
          rtt: { lastMs: 3.08, samples: [2.9, 3.2, 3.08] },
          stat: { sent: 2, replies: 2 },
        },
        {
          rdns: "ae-1.r01.tokyjp05.jp.bb.gin.ntt.net",
          ip: "129.250.6.121",
          isp: { ispName: "NTT", asn: "AS2914" },
          rtt: { lastMs: 3.12, samples: [3.0, 3.5, 3.12] },
          stat: { sent: 3, replies: 3 },
        },
        {
          rdns: "ae-2.r01.tokyjp05.jp.bb.gin.ntt.net",
          ip: "129.250.6.125",
          isp: { ispName: "NTT", asn: "AS2914" },
          rtt: { lastMs: 3.08, samples: [2.9, 3.2, 3.08] },
          stat: { sent: 2, replies: 2 },
        },
        {
          rdns: "ae-1.r01.tokyjp05.jp.bb.gin.ntt.net",
          ip: "129.250.6.121",
          isp: { ispName: "NTT", asn: "AS2914" },
          rtt: { lastMs: 3.12, samples: [3.0, 3.5, 3.12] },
          stat: { sent: 3, replies: 3 },
        },
        {
          rdns: "ae-2.r01.tokyjp05.jp.bb.gin.ntt.net",
          ip: "129.250.6.125",
          isp: { ispName: "NTT", asn: "AS2914" },
          rtt: { lastMs: 3.08, samples: [2.9, 3.2, 3.08] },
          stat: { sent: 2, replies: 2 },
        },
        {
          rdns: "ae-1.r01.tokyjp05.jp.bb.gin.ntt.net",
          ip: "129.250.6.121",
          isp: { ispName: "NTT", asn: "AS2914" },
          rtt: { lastMs: 3.12, samples: [3.0, 3.5, 3.12] },
          stat: { sent: 3, replies: 3 },
        },
        {
          rdns: "ae-2.r01.tokyjp05.jp.bb.gin.ntt.net",
          ip: "129.250.6.125",
          isp: { ispName: "NTT", asn: "AS2914" },
          rtt: { lastMs: 3.08, samples: [2.9, 3.2, 3.08] },
          stat: { sent: 2, replies: 2 },
        },
        {
          rdns: "ae-1.r01.tokyjp05.jp.bb.gin.ntt.net",
          ip: "129.250.6.121",
          isp: { ispName: "NTT", asn: "AS2914" },
          rtt: { lastMs: 3.12, samples: [3.0, 3.5, 3.12] },
          stat: { sent: 3, replies: 3 },
        },
        {
          rdns: "ae-2.r01.tokyjp05.jp.bb.gin.ntt.net",
          ip: "129.250.6.125",
          isp: { ispName: "NTT", asn: "AS2914" },
          rtt: { lastMs: 3.08, samples: [2.9, 3.2, 3.08] },
          stat: { sent: 2, replies: 2 },
        },
        {
          rdns: "ae-1.r01.tokyjp05.jp.bb.gin.ntt.net",
          ip: "129.250.6.121",
          isp: { ispName: "NTT", asn: "AS2914" },
          rtt: { lastMs: 3.12, samples: [3.0, 3.5, 3.12] },
          stat: { sent: 3, replies: 3 },
        },
        {
          rdns: "ae-2.r01.tokyjp05.jp.bb.gin.ntt.net",
          ip: "129.250.6.125",
          isp: { ispName: "NTT", asn: "AS2914" },
          rtt: { lastMs: 3.08, samples: [2.9, 3.2, 3.08] },
          stat: { sent: 2, replies: 2 },
        },
        {
          rdns: "ae-1.r01.tokyjp05.jp.bb.gin.ntt.net",
          ip: "129.250.6.121",
          isp: { ispName: "NTT", asn: "AS2914" },
          rtt: { lastMs: 3.12, samples: [3.0, 3.5, 3.12] },
          stat: { sent: 3, replies: 3 },
        },
        {
          rdns: "ae-2.r01.tokyjp05.jp.bb.gin.ntt.net",
          ip: "129.250.6.125",
          isp: { ispName: "NTT", asn: "AS2914" },
          rtt: { lastMs: 3.08, samples: [2.9, 3.2, 3.08] },
          stat: { sent: 2, replies: 2 },
        },
        {
          rdns: "ae-1.r01.tokyjp05.jp.bb.gin.ntt.net",
          ip: "129.250.6.121",
          isp: { ispName: "NTT", asn: "AS2914" },
          rtt: { lastMs: 3.12, samples: [3.0, 3.5, 3.12] },
          stat: { sent: 3, replies: 3 },
        },
        {
          rdns: "ae-2.r01.tokyjp05.jp.bb.gin.ntt.net",
          ip: "129.250.6.125",
          isp: { ispName: "NTT", asn: "AS2914" },
          rtt: { lastMs: 3.08, samples: [2.9, 3.2, 3.08] },
          stat: { sent: 2, replies: 2 },
        },
        {
          rdns: "ae-1.r01.tokyjp05.jp.bb.gin.ntt.net",
          ip: "129.250.6.121",
          isp: { ispName: "NTT", asn: "AS2914" },
          rtt: { lastMs: 3.12, samples: [3.0, 3.5, 3.12] },
          stat: { sent: 3, replies: 3 },
        },
        {
          rdns: "ae-2.r01.tokyjp05.jp.bb.gin.ntt.net",
          ip: "129.250.6.125",
          isp: { ispName: "NTT", asn: "AS2914" },
          rtt: { lastMs: 3.08, samples: [2.9, 3.2, 3.08] },
          stat: { sent: 2, replies: 2 },
        },
        {
          rdns: "ae-1.r01.tokyjp05.jp.bb.gin.ntt.net",
          ip: "129.250.6.121",
          isp: { ispName: "NTT", asn: "AS2914" },
          rtt: { lastMs: 3.12, samples: [3.0, 3.5, 3.12] },
          stat: { sent: 3, replies: 3 },
        },
        {
          rdns: "ae-2.r01.tokyjp05.jp.bb.gin.ntt.net",
          ip: "129.250.6.125",
          isp: { ispName: "NTT", asn: "AS2914" },
          rtt: { lastMs: 3.08, samples: [2.9, 3.2, 3.08] },
          stat: { sent: 2, replies: 2 },
        },
        {
          rdns: "ae-1.r01.tokyjp05.jp.bb.gin.ntt.net",
          ip: "129.250.6.121",
          isp: { ispName: "NTT", asn: "AS2914" },
          rtt: { lastMs: 3.12, samples: [3.0, 3.5, 3.12] },
          stat: { sent: 3, replies: 3 },
        },
        {
          rdns: "ae-2.r01.tokyjp05.jp.bb.gin.ntt.net",
          ip: "129.250.6.125",
          isp: { ispName: "NTT", asn: "AS2914" },
          rtt: { lastMs: 3.08, samples: [2.9, 3.2, 3.08] },
          stat: { sent: 2, replies: 2 },
        },
        {
          rdns: "ae-1.r01.tokyjp05.jp.bb.gin.ntt.net",
          ip: "129.250.6.121",
          isp: { ispName: "NTT", asn: "AS2914" },
          rtt: { lastMs: 3.12, samples: [3.0, 3.5, 3.12] },
          stat: { sent: 3, replies: 3 },
        },
        {
          rdns: "ae-2.r01.tokyjp05.jp.bb.gin.ntt.net",
          ip: "129.250.6.125",
          isp: { ispName: "NTT", asn: "AS2914" },
          rtt: { lastMs: 3.08, samples: [2.9, 3.2, 3.08] },
          stat: { sent: 2, replies: 2 },
        },
        {
          rdns: "ae-1.r01.tokyjp05.jp.bb.gin.ntt.net",
          ip: "129.250.6.121",
          isp: { ispName: "NTT", asn: "AS2914" },
          rtt: { lastMs: 3.12, samples: [3.0, 3.5, 3.12] },
          stat: { sent: 3, replies: 3 },
        },
        {
          rdns: "ae-2.r01.tokyjp05.jp.bb.gin.ntt.net",
          ip: "129.250.6.125",
          isp: { ispName: "NTT", asn: "AS2914" },
          rtt: { lastMs: 3.08, samples: [2.9, 3.2, 3.08] },
          stat: { sent: 2, replies: 2 },
        },
        {
          rdns: "ae-1.r01.tokyjp05.jp.bb.gin.ntt.net",
          ip: "129.250.6.121",
          isp: { ispName: "NTT", asn: "AS2914" },
          rtt: { lastMs: 3.12, samples: [3.0, 3.5, 3.12] },
          stat: { sent: 3, replies: 3 },
        },
        {
          rdns: "ae-2.r01.tokyjp05.jp.bb.gin.ntt.net",
          ip: "129.250.6.125",
          isp: { ispName: "NTT", asn: "AS2914" },
          rtt: { lastMs: 3.08, samples: [2.9, 3.2, 3.08] },
          stat: { sent: 2, replies: 2 },
        },
        {
          rdns: "ae-1.r01.tokyjp05.jp.bb.gin.ntt.net",
          ip: "129.250.6.121",
          isp: { ispName: "NTT", asn: "AS2914" },
          rtt: { lastMs: 3.12, samples: [3.0, 3.5, 3.12] },
          stat: { sent: 3, replies: 3 },
        },
        {
          rdns: "ae-2.r01.tokyjp05.jp.bb.gin.ntt.net",
          ip: "129.250.6.125",
          isp: { ispName: "NTT", asn: "AS2914" },
          rtt: { lastMs: 3.08, samples: [2.9, 3.2, 3.08] },
          stat: { sent: 2, replies: 2 },
        },
        {
          rdns: "ae-1.r01.tokyjp05.jp.bb.gin.ntt.net",
          ip: "129.250.6.121",
          isp: { ispName: "NTT", asn: "AS2914" },
          rtt: { lastMs: 3.12, samples: [3.0, 3.5, 3.12] },
          stat: { sent: 3, replies: 3 },
        },
        {
          rdns: "ae-2.r01.tokyjp05.jp.bb.gin.ntt.net",
          ip: "129.250.6.125",
          isp: { ispName: "NTT", asn: "AS2914" },
          rtt: { lastMs: 3.08, samples: [2.9, 3.2, 3.08] },
          stat: { sent: 2, replies: 2 },
        },
        {
          rdns: "ae-1.r01.tokyjp05.jp.bb.gin.ntt.net",
          ip: "129.250.6.121",
          isp: { ispName: "NTT", asn: "AS2914" },
          rtt: { lastMs: 3.12, samples: [3.0, 3.5, 3.12] },
          stat: { sent: 3, replies: 3 },
        },
        {
          rdns: "ae-2.r01.tokyjp05.jp.bb.gin.ntt.net",
          ip: "129.250.6.125",
          isp: { ispName: "NTT", asn: "AS2914" },
          rtt: { lastMs: 3.08, samples: [2.9, 3.2, 3.08] },
          stat: { sent: 2, replies: 2 },
        },
        {
          rdns: "ae-1.r01.tokyjp05.jp.bb.gin.ntt.net",
          ip: "129.250.6.121",
          isp: { ispName: "NTT", asn: "AS2914" },
          rtt: { lastMs: 3.12, samples: [3.0, 3.5, 3.12] },
          stat: { sent: 3, replies: 3 },
        },
        {
          rdns: "ae-2.r01.tokyjp05.jp.bb.gin.ntt.net",
          ip: "129.250.6.125",
          isp: { ispName: "NTT", asn: "AS2914" },
          rtt: { lastMs: 3.08, samples: [2.9, 3.2, 3.08] },
          stat: { sent: 2, replies: 2 },
        },
        {
          rdns: "ae-1.r01.tokyjp05.jp.bb.gin.ntt.net",
          ip: "129.250.6.121",
          isp: { ispName: "NTT", asn: "AS2914" },
          rtt: { lastMs: 3.12, samples: [3.0, 3.5, 3.12] },
          stat: { sent: 3, replies: 3 },
        },
        {
          rdns: "ae-2.r01.tokyjp05.jp.bb.gin.ntt.net",
          ip: "129.250.6.125",
          isp: { ispName: "NTT", asn: "AS2914" },
          rtt: { lastMs: 3.08, samples: [2.9, 3.2, 3.08] },
          stat: { sent: 2, replies: 2 },
        },
        {
          rdns: "ae-1.r01.tokyjp05.jp.bb.gin.ntt.net",
          ip: "129.250.6.121",
          isp: { ispName: "NTT", asn: "AS2914" },
          rtt: { lastMs: 3.12, samples: [3.0, 3.5, 3.12] },
          stat: { sent: 3, replies: 3 },
        },
        {
          rdns: "ae-2.r01.tokyjp05.jp.bb.gin.ntt.net",
          ip: "129.250.6.125",
          isp: { ispName: "NTT", asn: "AS2914" },
          rtt: { lastMs: 3.08, samples: [2.9, 3.2, 3.08] },
          stat: { sent: 2, replies: 2 },
        },
        {
          rdns: "ae-1.r01.tokyjp05.jp.bb.gin.ntt.net",
          ip: "129.250.6.121",
          isp: { ispName: "NTT", asn: "AS2914" },
          rtt: { lastMs: 3.12, samples: [3.0, 3.5, 3.12] },
          stat: { sent: 3, replies: 3 },
        },
        {
          rdns: "ae-2.r01.tokyjp05.jp.bb.gin.ntt.net",
          ip: "129.250.6.125",
          isp: { ispName: "NTT", asn: "AS2914" },
          rtt: { lastMs: 3.08, samples: [2.9, 3.2, 3.08] },
          stat: { sent: 2, replies: 2 },
        },
        {
          rdns: "ae-1.r01.tokyjp05.jp.bb.gin.ntt.net",
          ip: "129.250.6.121",
          isp: { ispName: "NTT", asn: "AS2914" },
          rtt: { lastMs: 3.12, samples: [3.0, 3.5, 3.12] },
          stat: { sent: 3, replies: 3 },
        },
        {
          rdns: "ae-2.r01.tokyjp05.jp.bb.gin.ntt.net",
          ip: "129.250.6.125",
          isp: { ispName: "NTT", asn: "AS2914" },
          rtt: { lastMs: 3.08, samples: [2.9, 3.2, 3.08] },
          stat: { sent: 2, replies: 2 },
        },
      ],
    },
    {
      ttl: 5,
      peers: [
        {
          rdns: "ae-10.r24.tokyjp05.jp.bb.gin.ntt.net",
          ip: "129.250.2.145",
          rtt: { lastMs: 5.82, samples: [5.5, 6.1, 5.82, 5.7, 5.9] },
          stat: { sent: 5, replies: 5 },
        },
      ],
    },
    // Trans-Pacific Crossing (Latency jump)
    {
      ttl: 6,
      peers: [
        {
          rdns: "ae-5.r21.osakjp02.jp.bb.gin.ntt.net",
          ip: "129.250.4.110",
          rtt: { lastMs: 12.4, samples: [12.1, 13.5, 12.4] },
          stat: { sent: 5, replies: 5 },
        },
      ],
    },
    {
      ttl: 7,
      peers: [
        {
          rdns: "ae-0.r20.osakjp02.jp.bb.gin.ntt.net",
          ip: "129.250.2.12",
          rtt: { lastMs: 15.1, samples: [14.8, 15.5, 15.1] },
          stat: { sent: 5, replies: 5 },
        },
      ],
    },
    {
      ttl: 8,
      peers: [{ timeout: true, ip: "" }],
    },
    {
      ttl: 9,
      peers: [
        {
          rdns: "ae-11.r21.snjsca04.us.bb.gin.ntt.net",
          ip: "129.250.3.45",
          loc: { city: "San Jose", countryAlpha2: "US" },
          rtt: { lastMs: 105.4, samples: [104.2, 108.1, 105.4, 105.0, 106.2] },
          stat: { sent: 10, replies: 10 },
        },
      ],
    },
    // US Backbone (San Jose -> Dallas)
    {
      ttl: 10,
      peers: [
        {
          rdns: "ae-2.r20.snjsca04.us.bb.gin.ntt.net",
          ip: "129.250.6.1",
          rtt: { lastMs: 106.2, samples: [105.9, 107.2, 106.2] },
          stat: { sent: 5, replies: 5 },
        },
      ],
    },
    {
      ttl: 11,
      peers: [
        {
          rdns: "be-3112.ccr41.sjc01.atlas.cogentco.com",
          ip: "154.54.44.113",
          isp: { ispName: "Cogent", asn: "AS174" },
          rtt: { lastMs: 108.7, samples: [108.1, 110.2, 108.7] },
          stat: { sent: 5, replies: 5 },
        },
      ],
    },
    {
      ttl: 12,
      peers: [
        {
          rdns: "be-3142.ccr21.sfo01.atlas.cogentco.com",
          ip: "154.54.43.14",
          rtt: { lastMs: 112.3, samples: [111.5, 114.1, 112.3] },
          stat: { sent: 5, replies: 5 },
        },
      ],
    },
    {
      ttl: 13,
      peers: [
        {
          rdns: "be-2831.ccr41.slc01.atlas.cogentco.com",
          ip: "154.54.44.102",
          loc: { city: "Salt Lake City", countryAlpha2: "US" },
          rtt: { lastMs: 125.1, samples: [124.2, 126.8, 125.1] },
          stat: { sent: 5, replies: 5 },
        },
      ],
    },
    {
      ttl: 14,
      peers: [
        {
          rdns: "be-3035.ccr21.den01.atlas.cogentco.com",
          ip: "154.54.5.110",
          loc: { city: "Denver", countryAlpha2: "US" },
          rtt: { lastMs: 138.4, samples: [137.5, 140.2, 138.4] },
          stat: { sent: 5, replies: 5 },
        },
      ],
    },
    {
      ttl: 15,
      peers: [
        {
          rdns: "be-3038.ccr41.ord01.atlas.cogentco.com",
          ip: "154.54.7.129",
          loc: { city: "Chicago", countryAlpha2: "US" },
          rtt: { lastMs: 152.9, samples: [151.8, 154.5, 152.9] },
          stat: { sent: 5, replies: 5 },
        },
      ],
    },
    // New York / Trans-Atlantic Jump
    {
      ttl: 16,
      peers: [
        {
          rdns: "be-2817.ccr21.cle01.atlas.cogentco.com",
          ip: "154.54.31.213",
          rtt: { lastMs: 158.2, samples: [157.1, 160.4, 158.2] },
          stat: { sent: 5, replies: 5 },
        },
      ],
    },
    {
      ttl: 17,
      peers: [
        {
          rdns: "be-2819.ccr41.jfk02.atlas.cogentco.com",
          ip: "154.54.47.118",
          loc: { city: "New York", countryAlpha2: "US" },
          rtt: { lastMs: 168.4, samples: [167.5, 172.1, 168.4] },
          stat: { sent: 5, replies: 5 },
        },
      ],
    },
    {
      ttl: 18,
      peers: [{ timeout: true, ip: "" }],
    },
    {
      ttl: 19,
      peers: [
        {
          rdns: "be-1107.ccr41.lon13.atlas.cogentco.com",
          ip: "154.54.44.161",
          loc: { city: "London", countryAlpha2: "GB" },
          rtt: { lastMs: 235.1, samples: [234.2, 238.9, 235.1, 236.0, 235.5] },
          stat: { sent: 10, replies: 10 },
        },
      ],
    },
    // London Internal
    {
      ttl: 20,
      peers: [
        {
          rdns: "be-2101.ccr21.lon01.atlas.cogentco.com",
          ip: "130.117.3.1",
          isp: { ispName: "Cogent", asn: "AS174" },
          rtt: { lastMs: 236.4, samples: [235.5, 237.8, 236.4] },
          stat: { sent: 5, replies: 5 },
        },
      ],
    },
    {
      ttl: 21,
      peers: [
        {
          rdns: "linx-lon-gw1.global-services.uk",
          ip: "195.66.224.12",
          rtt: { lastMs: 238.7, samples: [238.1, 240.2, 238.7] },
          stat: { sent: 5, replies: 5 },
        },
      ],
    },
    {
      ttl: 22,
      peers: [
        {
          rdns: "cr01.lon.global-services.uk",
          ip: "62.115.14.22",
          isp: { ispName: "Global Services UK", asn: "AS5567" },
          rtt: { lastMs: 239.1, samples: [238.8, 241.0, 239.1] },
          stat: { sent: 5, replies: 5 },
        },
      ],
    },
    {
      ttl: 23,
      peers: [{ timeout: true, ip: "" }],
    },
    {
      ttl: 24,
      peers: [
        {
          rdns: "er05.lon.global-services.uk",
          ip: "62.115.120.4",
          rtt: { lastMs: 241.5, samples: [240.5, 243.2, 241.5] },
          stat: { sent: 5, replies: 5 },
        },
      ],
    },
    {
      ttl: 25,
      peers: [
        {
          ip: "10.255.4.12",
          rtt: { lastMs: 242.0, samples: [241.8, 242.5, 242.0] },
          stat: { sent: 5, replies: 5 },
        },
      ],
    },
    {
      ttl: 26,
      peers: [
        {
          ip: "10.255.4.18",
          rtt: { lastMs: 242.4, samples: [242.0, 243.1, 242.4] },
          stat: { sent: 5, replies: 5 },
        },
      ],
    },
    {
      ttl: 27,
      peers: [
        {
          ip: "10.80.12.1",
          rtt: { lastMs: 243.2, samples: [242.8, 244.5, 243.2] },
          stat: { sent: 5, replies: 5 },
        },
      ],
    },
    {
      ttl: 28,
      peers: [
        {
          rdns: "lb-01-public.global-services.uk",
          ip: "85.25.122.40",
          rtt: { lastMs: 244.8, samples: [244.1, 246.2, 244.8] },
          stat: { sent: 5, replies: 4 },
        },
      ], // Simulating 20% loss
    },
    {
      ttl: 29,
      peers: [{ timeout: true, ip: "" }],
    },
    {
      ttl: 30,
      peers: [
        {
          rdns: "api.global-services.uk",
          ip: "85.25.122.100",
          rtt: { lastMs: 245.2, samples: [244.5, 246.8, 245.2, 245.0, 245.7] },
          stat: { sent: 5, replies: 5 },
        },
      ],
    },
  ],
};

export default function Page() {
  const [show, setShow] = useState(true);
  const canvasRef = useRef<HTMLCanvasElement>(null);

  const exampleReport = useMemo(() => {
    return LONG_TRACEROUTE_DATA;
    const exampleReport: TracerouteReport = {
      date: new Date().valueOf(),
      sources: [
        {
          nodeName: "LAX1",
          isp: { ispName: "Cloudflare", asn: "AS13335" },
          loc: { countryAlpha2: "US", city: "Los Angeles" },
        },
      ],
      destination: "www.example.com",
      mode: "udp",
      hops: [
        {
          ttl: 1,
          peers: [
            {
              ip: "203.0.113.1",
              rtt: { lastMs: 0.5, samples: [0.4, 0.6, 0.5, 0.5, 0.5] },
              stat: { sent: 5, replies: 5 },
            },
          ],
        },
        {
          ttl: 2,
          peers: [
            {
              rdns: "gw01.lax.example.com",
              ip: "198.51.100.1",
              isp: { ispName: "Level 3 Communications", asn: "AS3356" },
              rtt: { lastMs: 1.2, samples: [1.1, 1.3, 1.2, 1.2, 1.1] },
              stat: { sent: 5, replies: 5 },
            },
          ],
        },
        {
          ttl: 3,
          peers: [
            {
              rdns: "ae5-1234.lax10.core.example.net",
              ip: "192.0.2.1",
              isp: { ispName: "Cogent Communications", asn: "AS174" },
              loc: { city: "Los Angeles", countryAlpha2: "US" },
              rtt: { lastMs: 2.8, samples: [2.5, 3.1, 2.8, 2.7, 3.0] },
              stat: { sent: 5, replies: 5 },
            },
          ],
        },
        {
          ttl: 4,
          peers: [
            {
              rdns: "be301.dal10.example.com",
              ip: "203.0.113.10",
              isp: { ispName: "Hurricane Electric", asn: "AS6939" },
              loc: { city: "Dallas", countryAlpha2: "US" },
              rtt: { lastMs: 18.5, samples: [17.8, 19.2, 18.5, 18.3, 18.7] },
              stat: { sent: 5, replies: 5 },
            },
          ],
        },
        {
          ttl: 5,
          peers: [
            {
              rdns: "ae0-123.atl10.example.net",
              ip: "192.0.2.50",
              isp: { ispName: "NTT Communications", asn: "AS2914" },
              loc: { city: "Atlanta", countryAlpha2: "US" },
              rtt: { lastMs: 32.1, samples: [31.5, 32.8, 32.1, 31.9, 32.3] },
              stat: { sent: 5, replies: 5 },
            },
          ],
        },
        {
          ttl: 6,
          peers: [
            {
              ip: "198.51.100.99",
              isp: { ispName: "Example ISP Inc.", asn: "AS64512" },
              rtt: { lastMs: 48.2, samples: [47.5, 49.0, 48.2, 48.0, 48.5] },
              stat: { sent: 5, replies: 5 },
            },
          ],
        },
        {
          ttl: 7,
          peers: [
            {
              rdns: "edge-router.example.com",
              ip: "203.0.113.254",
              isp: { ispName: "Example Hosting", asn: "AS64513" },
              rtt: { lastMs: 52.3, samples: [51.8, 53.1, 52.3, 52.0, 52.5] },
              stat: { sent: 5, replies: 5 },
            },
          ],
        },
        {
          ttl: 8,
          peers: [
            {
              timeout: true,
              ip: "",
            },
          ],
        },
        {
          ttl: 9,
          peers: [
            {
              rdns: "www.example.com",
              ip: "93.184.216.34",
              isp: { ispName: "Edgecast Networks", asn: "AS15133" },
              loc: { city: "New York", countryAlpha2: "US" },
              rtt: { lastMs: 65.4, samples: [64.8, 66.2, 65.4, 65.1, 65.7] },
              stat: { sent: 5, replies: 5 },
            },
          ],
        },
        // 添加一个ECMP场景（同一跳有多个peer）
        {
          ttl: 10,
          peers: [
            {
              rdns: "loadbalancer1.example.com",
              ip: "192.0.2.100",
              isp: { ispName: "Cloudflare", asn: "AS13335" },
              loc: { city: "New York", countryAlpha2: "US" },
              rtt: { lastMs: 66.1, samples: [65.5, 66.8, 66.1, 65.9, 66.3] },
              stat: { sent: 5, replies: 5 },
            },
            {
              rdns: "loadbalancer2.example.com",
              ip: "192.0.2.101",
              isp: { ispName: "Cloudflare", asn: "AS13335" },
              loc: { city: "New York", countryAlpha2: "US" },
              rtt: { lastMs: 66.0, samples: [65.4, 66.7, 66.0, 65.8, 66.2] },
              stat: { sent: 5, replies: 5 },
            },
            {
              rdns: "loadbalancer3.example.com",
              ip: "192.0.2.102",
              isp: { ispName: "Cloudflare", asn: "AS13335" },
              loc: { city: "New York", countryAlpha2: "US" },
              rtt: { lastMs: 65.9, samples: [65.3, 66.6, 65.9, 65.7, 66.1] },
              stat: { sent: 5, replies: 5 },
            },
          ],
        },
      ],
    };
    return exampleReport;
  }, []);

  const { preamble, tabularData } = useMemo(() => {
    return renderTracerouteReport(exampleReport);
  }, [exampleReport]);

  return (
    <Box>
      <Button onClick={() => setShow((show) => !show)}>Open</Button>
      <Dialog
        open={show}
        onClose={() => {
          setShow(false);
        }}
        maxWidth={"md"}
        fullWidth
      >
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
                onClick={() => {
                  const canvasEle = canvasRef.current;
                  if (!canvasEle) {
                    console.error("no canvas element found.");
                  }
                  const mimeType = "image/png";
                  canvasEle!.toBlob((blob) => {
                    console.log("[dbg] blob:", blob);
                    if (!blob) {
                      console.error("cant export canvas to blob");
                    }
                    let fname = `trace-${new Date().toISOString()}.png`;
                    fname = fname.replaceAll(" ", "_");
                    const f = new File([blob!], fname, { type: mimeType });
                    const url = URL.createObjectURL(f);
                    const aEle = window.document.createElement("a");
                    aEle.setAttribute("href", url);
                    aEle.setAttribute("download", f.name);
                    aEle.click();
                  }, mimeType);
                }}
              >
                <SaveIcon />
              </IconButton>
            </Tooltip>
          </Box>
        </DialogTitle>
        <DialogContent
          sx={{ paddingLeft: 0, paddingRight: 0, paddingBottom: 0 }}
        >
          <CanvasTable
            preamble={preamble}
            tabularData={tabularData}
            canvasRef={canvasRef as any}
          />
        </DialogContent>
      </Dialog>
    </Box>
  );
}
