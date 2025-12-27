"use client";

import {
  CSSProperties,
  Fragment,
  RefObject,
  useEffect,
  useMemo,
  useState,
} from "react";
import worldMapAny from "./worldmap.json";
import { Box, Tooltip } from "@mui/material";

// format: [longitude, latitude]
type LonLat = number[];

type Polygon = LonLat[];

type Geometry = {
  type: "Polygon" | "MultiPolygon";

  coordinates:
    | Polygon
    | Polygon[]
    | Polygon[][]
    | Polygon[][][]
    | Polygon[][][][][];
};

type Feature = {
  type: "Feature";
  geometry: Geometry;
  properties: Record<string, any>;
};

type FeatureCollection = {
  type: "FeatureCollection";
  features: Feature[];
};

type FlatShape = {
  groupId: number;
  feature: Feature;
  polygon: Polygon;
};

function isPolygon(polygon: any): boolean {
  if (Array.isArray(polygon)) {
    if (polygon.length > 0) {
      if (Array.isArray(polygon[0])) {
        if (polygon[0].length === 2) {
          if (
            typeof polygon[0][0] === "number" &&
            typeof polygon[0][1] === "number"
          ) {
            return true;
          }
        }
      }
    }
  }
  return false;
}

function* yieldPolygons(
  polygonOrPolygons: any
): Generator<Polygon, void, unknown> {
  if (isPolygon(polygonOrPolygons)) {
    yield polygonOrPolygons as Polygon;
  } else if (Array.isArray(polygonOrPolygons)) {
    for (const polygonany of polygonOrPolygons) {
      yield* yieldPolygons(polygonany);
    }
  }
}

function toFlatShape(features: Feature[]): FlatShape[] {
  const flatShapes: FlatShape[] = [];
  let groupId = 0;
  for (const feature of features) {
    if (feature.geometry?.coordinates) {
      for (const polygon of yieldPolygons(feature.geometry.coordinates)) {
        flatShapes.push({
          groupId,
          feature,
          polygon,
        });
      }
    }
    groupId++;
  }
  return flatShapes;
}

type Projector = (point: LonLat) => [number, number];

function getProjector(canvasX: number, canvasY: number): Projector {
  return (point: LonLat) => {
    const [longitude, latitude] = point;
    const x = (longitude - -180) * (canvasX / 360);
    const y = (180 + (latitude - -90) * -1) * (canvasY / 180);
    return [x, y];
  };
}

function RenderPolygon(props: {
  polygon: Polygon;
  projector: Projector;
  fill: CSSProperties["fill"];
}) {
  const { polygon, projector, fill } = props;
  return (
    <Fragment>
      <polygon
        points={[...polygon, polygon[0]]
          .map((point) => projector(point).join(","))
          .join(" ")}
        fill={fill}
        stroke="none"
      />
    </Fragment>
  );
}

function RenderPolygons(props: {
  shapes: FlatShape[];
  projector: Projector;
  fill: CSSProperties["fill"];
}) {
  const { shapes, projector, fill } = props;
  return (
    <Fragment>
      {shapes.map((shape, i) => (
        <RenderPolygon
          key={i}
          polygon={shape.polygon}
          projector={projector}
          fill={fill}
        />
      ))}
    </Fragment>
  );
}

export type Marker = {
  lonLat: LonLat;
  fill?: CSSProperties["fill"];
  radius?: number;
  strokeWidth?: number;
  stroke?: CSSProperties["stroke"];
  tooltip?: string;
};

function RenderMarker(props: { marker: Marker; projector: Projector }) {
  const { marker, projector } = props;
  const [x, y] = projector(marker.lonLat);
  const [open, setOpen] = useState(false);
  return (
    <Fragment>
      <Tooltip
        title={<Box sx={{ whiteSpace: "pre-wrap" }}>{marker.tooltip}</Box>}
        open={open}
        onOpen={() => {
          if (marker.tooltip) {
            setOpen(true);
          }
        }}
        onClose={() => setOpen(false)}
      >
        <circle
          strokeWidth={marker.strokeWidth}
          stroke={marker.stroke}
          cx={x}
          cy={y}
          r={marker.radius}
          fill={marker.fill}
        />
      </Tooltip>
    </Fragment>
  );
}

export function WorldMap(props: {
  canvasWidth: number;
  canvasHeight: number;
  fill: CSSProperties["fill"];
  markers: Marker[];
  viewBox?: string;
  canvasSvgRef?: RefObject<SVGSVGElement>;
}) {
  const {
    canvasWidth: canvasX,
    canvasHeight: canvasY,
    fill,
    markers,
    viewBox,
    canvasSvgRef,
  } = props;
  const flatShapes = useMemo(() => {
    const worldMap = worldMapAny as FeatureCollection;
    const flatShapes = toFlatShape(worldMap.features);
    console.log("[dbg] flatShapes", flatShapes);
    return flatShapes;
  }, [worldMapAny]);

  const projector = useMemo(
    () => getProjector(canvasX, canvasY),
    [canvasX, canvasY]
  );

  return (
    <Fragment>
      <Box sx={{ height: "100%" }}>
        <svg
          ref={canvasSvgRef}
          viewBox={viewBox}
          width="100%"
          height="100%"
          style={{ overflow: "hidden" }}
        >
          <RenderPolygons
            shapes={flatShapes}
            projector={projector}
            fill={fill}
          />
          {markers.map((marker, i) => (
            <RenderMarker key={i} marker={marker} projector={projector} />
          ))}
        </svg>
      </Box>
    </Fragment>
  );
}
