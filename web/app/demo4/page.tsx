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
import { Fragment, useEffect, useRef, useState } from "react";
import SaveIcon from "@mui/icons-material/Save";

type LineDrawCtx = {
  fillStyle?: CanvasPattern | string | CanvasGradient;
  baseline?: CanvasTextBaseline;
  font?: string;
  y: number;
  lineGap: number;
  lastMetrics?: TextMetrics;
  x: number;
  maxWidth?: number;
  maxRight?: number;
  maxHeight?: number;
};

function drawLine(
  lineCtx: LineDrawCtx,
  ctx: CanvasRenderingContext2D,
  line: string
): LineDrawCtx {
  if (lineCtx.baseline) {
    ctx.textBaseline = lineCtx.baseline;
  }
  if (lineCtx.fillStyle) {
    ctx.fillStyle = lineCtx.fillStyle;
  }
  if (lineCtx.font) {
    ctx.font = lineCtx.font;
  }
  ctx.fillText(line, lineCtx.x, lineCtx.y);
  const lineMs = ctx.measureText(line);
  const prevMaxWidth = lineCtx.maxWidth ?? 0;
  const maxWidth = Math.max(prevMaxWidth, lineMs.width);
  const prevMaxRight = lineCtx.maxRight ?? 0;
  const maxRight = Math.max(prevMaxRight, lineCtx.x + lineMs.width);
  const prevMaxHeight = lineCtx.maxHeight ?? 0;
  const newHeight =
    lineCtx.y +
    lineMs.fontBoundingBoxAscent +
    lineMs.fontBoundingBoxDescent +
    lineCtx.lineGap;
  const maxHeight = Math.max(prevMaxHeight, newHeight);
  console.log("[dbg] maxRight:", maxRight);
  console.log("[dbg] maxHeight:", maxHeight);
  return {
    ...lineCtx,
    y: newHeight,
    lastMetrics: lineMs,
    maxWidth,
    maxRight,
    maxHeight,
  };
}

function drawCursor(
  x: number,
  y: number,
  ctx: CanvasRenderingContext2D,
  w: number,
  h: number,
  color: CanvasPattern | string | CanvasGradient
): void {
  const currentFill = ctx.fillStyle;
  ctx.fillStyle = color;
  ctx.fillRect(x, y, w, h);
  ctx.fillStyle = currentFill;
}

type ColDrawCtx = {
  lineDrawCtx: LineDrawCtx;
  columnGap: number;
  y0?: number;
};

type Cell = {
  content: string;
  empty?: boolean;
};

type Col = Cell[];

type Row = Cell[];

function transposeTable(rows: Row[]): Col[] {
  if (rows.length === 0) {
    return [];
  }
  const nCols = rows[0].length;
  const cols: Col[] = [];
  for (let c = 0; c < nCols; c++) {
    const col: Col = [];
    for (let r = 0; r < rows.length; r++) {
      if (rows[r].length >= c + 1) {
        col.push(rows[r][c]);
      } else {
        col.push({ content: "", empty: true });
      }
    }
    cols.push(col);
  }
  return cols;
}

function drawCol(
  colCtx: ColDrawCtx,
  ctx: CanvasRenderingContext2D,
  column: Col
): ColDrawCtx {
  let newColCtx: ColDrawCtx = {
    ...colCtx,
    y0: colCtx.y0 ?? colCtx.lineDrawCtx.y,
  };
  newColCtx.lineDrawCtx = {
    ...newColCtx.lineDrawCtx,
    maxWidth: 0,
    y: newColCtx.y0!,
  };

  for (const cell of column) {
    newColCtx.lineDrawCtx = drawLine(newColCtx.lineDrawCtx, ctx, cell.content);
  }

  newColCtx.lineDrawCtx = {
    ...newColCtx.lineDrawCtx,
    x:
      newColCtx.lineDrawCtx.x +
      (newColCtx.lineDrawCtx.maxWidth ?? 0) +
      newColCtx.columnGap,
    maxWidth: 0,
  };

  return newColCtx;
}

function carriageReturn(lineCtx: LineDrawCtx): LineDrawCtx {
  return {
    ...lineCtx,
    x: 0,
  };
}

function Window(props: { canvasRef: React.RefObject<HTMLCanvasElement> }) {
  const [w, setW] = useState(0);
  const [h, setH] = useState(0);
  const [maxW, setMaxW] = useState(0);
  const [maxH, setMaxH] = useState(0);

  const dpi = window.devicePixelRatio;
  const R = Math.max(w * dpi, maxW);
  const H = Math.max(h * dpi, maxH);

  const boxRef = useRef<HTMLDivElement>(null);

  useEffect(() => {
    const ele = boxRef.current;
    if (!ele) {
      return;
    }

    const rect = ele.getBoundingClientRect();
    console.log("[dbg] rect:", rect);

    const obs = new ResizeObserver((entries) => {
      for (const ent of entries) {
        if (ent.target === ele) {
          console.log("[dbg] ResizeObserver entry:", ent);
          const cr = ent.contentRect;
          if (cr) {
            setW(cr.width);
            setH(cr.height);
          }
        }
      }
    });

    obs.observe(ele);

    return () => {
      obs.unobserve(ele);
    };
  });

  useEffect(() => {
    const ele = boxRef.current;
    if (!ele) {
      return;
    }

    const canvasEle = ele.querySelector("canvas");
    if (!canvasEle) {
      return;
    }

    const dpi = window.devicePixelRatio;

    // canvasEle.setAttribute("width", `${w * dpi}`);
    // canvasEle.setAttribute("height", `${h * dpi}`);

    const ctx = canvasEle.getContext("2d");
    if (!ctx) {
      return;
    }

    ctx.fillStyle = "#262626";
    ctx.fillRect(0, 0, R, H);

    const lines: string[] = [
      `Date: ${new Date().toISOString()}`,
      "Source: Node NYC1, AS65001 SOMEISP, SomeCity US",
      "Destination: pingable.burble.dn42",
      "Mode: ICMP",
    ];
    let lineCtx: LineDrawCtx = {
      fillStyle: "#fff",
      baseline: "top",
      font: `${16 * dpi}px sans-serif`,
      y: 0,
      lineGap: 6 * dpi,
      x: 0,
    };

    for (const line of lines) {
      lineCtx = drawLine(lineCtx, ctx, line);
    }

    // drawCursor(lineCtx, ctx, 15 * dpi, 20 * dpi, "#111");

    let colCtx: ColDrawCtx = {
      lineDrawCtx: lineCtx,
      columnGap: 30 * dpi,
    };

    const rows: Row[] = [
      [
        { content: "TTL" },
        { content: "Peers" },
        { content: "ISP" },
        { content: "Location" },
        { content: "RTTs (last min/med/max)" },
        { content: "Stat" },
      ],
      [
        { content: "1" },
        { content: "RFC1819 (192.168.1.1)" },
        { content: "" },
        { content: "" },
        { content: "10ms 1ms/5ms/11ms" },
        { content: "10 sent, 8 replies, 20% loss" },
      ],
      [
        { content: "" },
        { content: "RFC1819 (192.168.2.1)" },
        { content: "" },
        { content: "" },
        { content: "11ms 2ms/6ms/12ms" },
        { content: "9 sent, 7 replies, 22.22% loss" },
      ],
      [{ content: "" }, { content: "*" }],
      [
        { content: "" },
        { content: "10.147.0.1" },
        { content: "" },
        { content: "" },
        { content: "5ms 1ms/4ms/7ms" },
        { content: "8 sent, 7 replies, 12.5% loss" },
      ],
      [
        { content: "2" },
        { content: "h100.1e100.net (123.124.125.126)" },
        { content: "AS65001 [EXAMPLEISP]" },
        { content: "Frankfurt, DE" },
        { content: "10ms 8ms/10ms/11ms" },
        { content: "8 sent, 7 replies, 12.5 loss" },
      ],
      [
        { content: "" },
        { content: "h101.1e100.net (123.124.125.127)" },
        { content: "AS65001 [EXAMPLEISP]" },
        { content: "Frankfurt, DE" },
        { content: "10ms 8ms/10ms/11ms" },
        { content: "8 sent, 7 replies, 12.5 loss" },
      ],
      [
        { content: "3" },
        { content: "h100.1e101.net (124.125.126.127)" },
        { content: "AS65002 [EXAMPLEISP2]" },
        { content: "Frankfurt, DE" },
        { content: "12ms 8ms/12ms/14ms" },
        { content: "8 sent, 7 replies, 12.5 loss" },
      ],
      [
        { content: "" },
        { content: "124.125.126.128" },
        { content: "AS65002 [EXAMPLEISP2]" },
        { content: "Frankfurt, DE" },
        { content: "12ms 8ms/12ms/13ms" },
        { content: "8 sent, 7 replies, 12.5 loss" },
      ],
      [
        { content: "4" },
        { content: "bb1.dod.us(11.1.2.3)" },
        { content: "AS65003 [DoD]" },
        { content: "Washington DC, US" },
        { content: "112ms 81ms/121ms/141ms" },
        { content: "8 sent, 7 replies, 12.5 loss" },
      ],
      [
        { content: "" },
        { content: "bb2.dod.us(11.1.2.4)" },
        { content: "AS65003 [DoD]" },
        { content: "Washington DC, US" },
        { content: "112ms 81ms/121ms/131ms" },
        { content: "8 sent, 7 replies, 12.5 loss" },
      ],
      [
        { content: "" },
        { content: "bb3.dod.us(11.1.2.5)" },
        { content: "AS65003 [DoD]" },
        { content: "Washington DC, US" },
        { content: "112ms 81ms/121ms/131ms" },
        { content: "8 sent, 7 replies, 12.5 loss" },
      ],
      [
        { content: "4" },
        { content: "bb1.dod.us(11.1.2.3)" },
        { content: "AS65003 [DoD]" },
        { content: "Washington DC, US" },
        { content: "112ms 81ms/121ms/141ms" },
        { content: "8 sent, 7 replies, 12.5 loss" },
      ],
      [
        { content: "" },
        { content: "bb2.dod.us(11.1.2.4)" },
        { content: "AS65003 [DoD]" },
        { content: "Washington DC, US" },
        { content: "112ms 81ms/121ms/131ms" },
        { content: "8 sent, 7 replies, 12.5 loss" },
      ],
      [
        { content: "" },
        { content: "bb3.dod.us(11.1.2.5)" },
        { content: "AS65003 [DoD]" },
        { content: "Washington DC, US" },
        { content: "112ms 81ms/121ms/131ms" },
        { content: "8 sent, 7 replies, 12.5 loss" },
      ],
      [
        { content: "5" },
        { content: "bb1.dod.us(11.1.2.3)" },
        { content: "AS65003 [DoD]" },
        { content: "Washington DC, US" },
        { content: "112ms 81ms/121ms/141ms" },
        { content: "8 sent, 7 replies, 12.5 loss" },
      ],
      [
        { content: "" },
        { content: "bb2.dod.us(11.1.2.4)" },
        { content: "AS65003 [DoD]" },
        { content: "Washington DC, US" },
        { content: "112ms 81ms/121ms/131ms" },
        { content: "8 sent, 7 replies, 12.5 loss" },
      ],
      [
        { content: "" },
        { content: "bb3.dod.us(11.1.2.5)" },
        { content: "AS65003 [DoD]" },
        { content: "Washington DC, US" },
        { content: "112ms 81ms/121ms/131ms" },
        { content: "8 sent, 7 replies, 12.5 loss" },
      ],
      [
        { content: "6" },
        { content: "bb1.dod.us(11.1.2.3)" },
        { content: "AS65003 [DoD]" },
        { content: "Washington DC, US" },
        { content: "112ms 81ms/121ms/141ms" },
        { content: "8 sent, 7 replies, 12.5 loss" },
      ],
      [
        { content: "" },
        { content: "bb2.dod.us(11.1.2.4)" },
        { content: "AS65003 [DoD]" },
        { content: "Washington DC, US" },
        { content: "112ms 81ms/121ms/131ms" },
        { content: "8 sent, 7 replies, 12.5 loss" },
      ],
      [
        { content: "" },
        { content: "bb3.dod.us(11.1.2.5)" },
        { content: "AS65003 [DoD]" },
        { content: "Washington DC, US" },
        { content: "112ms 81ms/121ms/131ms" },
        { content: "8 sent, 7 replies, 12.5 loss" },
      ],
      [
        { content: "7" },
        { content: "bb1.dod.us(11.1.2.3)" },
        { content: "AS65003 [DoD]" },
        { content: "Washington DC, US" },
        { content: "112ms 81ms/121ms/141ms" },
        { content: "8 sent, 7 replies, 12.5 loss" },
      ],
      [
        { content: "" },
        { content: "bb2.dod.us(11.1.2.4)" },
        { content: "AS65003 [DoD]" },
        { content: "Washington DC, US" },
        { content: "112ms 81ms/121ms/131ms" },
        { content: "8 sent, 7 replies, 12.5 loss" },
      ],
      [
        { content: "" },
        { content: "bb3.dod.us(11.1.2.5)" },
        { content: "AS65003 [DoD]" },
        { content: "Washington DC, US" },
        { content: "112ms 81ms/121ms/131ms" },
        { content: "8 sent, 7 replies, 12.5 loss" },
      ],
      [
        { content: "8" },
        { content: "bb1.dod.us(11.1.2.3)" },
        { content: "AS65003 [DoD]" },
        { content: "Washington DC, US" },
        { content: "112ms 81ms/121ms/141ms" },
        { content: "8 sent, 7 replies, 12.5 loss" },
      ],
      [
        { content: "" },
        { content: "bb2.dod.us(11.1.2.4)" },
        { content: "AS65003 [DoD]" },
        { content: "Washington DC, US" },
        { content: "112ms 81ms/121ms/131ms" },
        { content: "8 sent, 7 replies, 12.5 loss" },
      ],
      [
        { content: "" },
        { content: "bb3.dod.us(11.1.2.5)" },
        { content: "AS65003 [DoD]" },
        { content: "Washington DC, US" },
        { content: "112ms 81ms/121ms/131ms" },
        { content: "8 sent, 7 replies, 12.5 loss" },
      ],
    ];
    const cols = transposeTable(rows);

    for (const col of cols) {
      colCtx = drawCol(colCtx, ctx, col);
    }

    colCtx = {
      ...colCtx,
      lineDrawCtx: carriageReturn(colCtx.lineDrawCtx),
    };

    drawCursor(
      colCtx.lineDrawCtx.x,
      colCtx.lineDrawCtx.y,
      ctx,
      15 * dpi,
      20 * dpi,
      "#111"
    );

    console.log("[dbg] paint.");
    const maxR = colCtx.lineDrawCtx.maxRight ?? 0;
    console.log("[dbg] maxRight:", maxR);
    const maxH = colCtx.lineDrawCtx.maxHeight ?? 0;
    console.log("[dbg] maxHeight:", maxH);

    setMaxH(maxH);
    setMaxW(maxR);
  });

  const [showDbg, setShowDbg] = useState(false);

  return (
    <Fragment>
      <Box
        ref={boxRef}
        sx={{
          height: "400px",
          overflow: "auto",
        }}
      >
        <canvas
          ref={props.canvasRef}
          style={{ width: "100%", height: "auto" }}
          width={R}
          height={H}
        ></canvas>
      </Box>
      {showDbg && (
        <Box
          sx={{
            paddingTop: 2,
            paddingLeft: 3,
            paddingRight: 3,
            display: "flex",
            gap: 2,
            flexWrap: "wrap",
          }}
        >
          <Box>W: {w}</Box>
          <Box>H: {h}</Box>
          <Box>MaxW: {maxW.toFixed(0)}</Box>
          <Box>MaxH: {maxH.toFixed(0)}</Box>
        </Box>
      )}
    </Fragment>
  );
}

export default function Page() {
  const [show, setShow] = useState(true);
  const canvasRef = useRef<HTMLCanvasElement>(null);

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
          <Window canvasRef={canvasRef as any} />
        </DialogContent>
      </Dialog>
    </Box>
  );
}
