"use client";

import {
  City,
  getViewBox,
  LonLat,
  Marker,
  Path,
  toGeodesicPaths,
  useCanvasSizing,
  WorldMap,
} from "@/components/worldmap";
import { Box } from "@mui/material";
import { CSSProperties, Fragment, useEffect, useState } from "react";
import { Quaternion, Vector3, Euler } from "three";

export default function DemoPage() {
  const [show, setShow] = useState(false);
  //   useEffect(() => {
  //     const ticker = window.setInterval(() => {
  //       setShow((prev) => !prev);
  //     }, 250);
  //     return () => {
  //       window.clearInterval(ticker);
  //     };
  //   }, []);

  const canvasWidth = 40000;
  const canvasHeight = 25000;

  const { canvasSvgRef, ratio = 1 } = useCanvasSizing(
    canvasWidth,
    canvasHeight,
    show,
    true
  );

  //   const fill: CSSProperties["fill"] = "hsl(202deg 32% 50%)";
  const fill: CSSProperties["fill"] = "#373737";

  const london: City = {
    name: "London",
    latLon: [51 + 30 / 60 + 26 / 3600, 0 + 7 / 60 + 39 / 3600],
  };

  const newYork: City = {
    name: "New York",
    latLon: [40 + 42 / 60 + 46 / 3600, -74 - 0 - 22 / 3600],
  };

  const beijing: City = {
    name: "Beijing",
    latLon: [39 + 54 / 60 + 26 / 3600, 116 + 23 / 60 + 22 / 3600],
  };

  const singapore: City = {
    name: "Singapore",
    latLon: [1 + 22 / 60 + 14 / 3600, 103 + 45 / 60 + 59 / 3600],
  };

  let extraPaths: Path[] = [];

  extraPaths = [
    ...toGeodesicPaths(beijing.latLon, newYork.latLon, 200),
    ...toGeodesicPaths(newYork.latLon, london.latLon, 200),
    ...toGeodesicPaths(london.latLon, beijing.latLon, 200),
    ...toGeodesicPaths(london.latLon, singapore.latLon, 200),
    ...toGeodesicPaths(singapore.latLon, beijing.latLon, 200),
  ].map((p) => ({ ...p, stroke: "green", strokeWidth: 4 * ratio }));

  return (
    <Box
      sx={{
        width: "100vw",
        height: "100vh",
        position: "fixed",
        top: 0,
        left: 0,
        overflow: "hidden",
        backgroundColor: "#ddd",
      }}
    >
      <WorldMap
        canvasSvgRef={canvasSvgRef as any}
        canvasWidth={canvasWidth}
        canvasHeight={canvasHeight}
        fill={fill}
        paths={extraPaths}
        markers={[
          {
            lonLat: [london.latLon[1], london.latLon[0]],
            fill: "green",
            radius: 8 * ratio,
            strokeWidth: 3 * ratio,
            stroke: "#fff",
            index: "TTL=1, London, fdda:1234::5678",
          },
          {
            lonLat: [newYork.latLon[1], newYork.latLon[0]],
            fill: "green",
            radius: 8 * ratio,
            strokeWidth: 3 * ratio,
            stroke: "#fff",
            index: "TTL=2, New York, 1.2.3.4",
          },
          {
            lonLat: [singapore.latLon[1], singapore.latLon[0]],
            fill: "green",
            radius: 8 * ratio,
            strokeWidth: 3 * ratio,
            stroke: "#fff",
            index: "TTL=3, Singapore, fdda:1234::5678",
          },
          {
            lonLat: [beijing.latLon[1], beijing.latLon[0]],
            fill: "green",
            radius: 8 * ratio,
            strokeWidth: 3 * ratio,
            stroke: "#fff",
            index: "TTL=4, Beijing, 192.168.1.1",
          },
        ]}
      />
    </Box>
  );
}
