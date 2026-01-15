"use client";

import {
  CSSProperties,
  Fragment,
  ReactNode,
  RefObject,
  useEffect,
  useMemo,
  useRef,
  useState,
} from "react";
import worldMapAny from "./worldmap.json";
import { Box, Tooltip } from "@mui/material";
import { Quaternion, Vector3 } from "three";

// format: [longitude, latitude]
export type LonLat = number[];

export type LatLon = number[];

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

export type City = {
  name: string;
  latLon: [number, number];
};

export function getQuatFromLat(lat: number): Quaternion {
  const latQuat = new Quaternion();
  latQuat.setFromAxisAngle(new Vector3(1, 0, 0), (-1 * lat * Math.PI) / 180);
  return latQuat;
}

export function getQuatFromLon(lon: number): Quaternion {
  const lonQuat = new Quaternion();
  lonQuat.setFromAxisAngle(new Vector3(0, 1, 0), (lon * Math.PI) / 180);
  return lonQuat;
}

export function latLonToQuat(lat: number, lon: number): Quaternion {
  const latQuat = getQuatFromLat(lat);
  const lonQuat = getQuatFromLon(lon);

  return lonQuat.clone().multiply(latQuat);
}

export const baseVector = new Vector3(0, 0, 1);

export function xyzToLatLon(xyz: Vector3): [number, number] {
  const x = xyz.x;
  const y = xyz.y;
  const z = xyz.z;

  const lat = (Math.atan(y / Math.sqrt(z * z + x * x)) * 180) / Math.PI;
  const lonDelta = (Math.atan(z / x) * 180) / Math.PI;
  let lon = lonDelta;
  if (x >= 0) {
    lon = 90 - lon;
  } else {
    lon = -1 * (90 + lon);
  }
  return [lat, lon];
}

function getGeodesicPoints(
  startPoint: Vector3,
  endPoint: Vector3,
  numPoints: number
): Vector3[] {
  const points = [];
  const quaternion = new Quaternion();

  // Calculate the quaternion representing the full rotation
  quaternion.setFromUnitVectors(startPoint, endPoint);

  for (let i = 0; i <= numPoints; i++) {
    const t = i / numPoints;
    const interpolatedQuaternion = new Quaternion()
      .identity()
      .slerp(quaternion, t);

    // Apply the interpolated quaternion to the starting point
    const geodesicPoint = startPoint
      .clone()
      .applyQuaternion(interpolatedQuaternion);

    // If your sphere has a specific radius, multiply the point's components by that radius
    // const radius = 10;
    // geodesicPoint.multiplyScalar(radius);

    points.push(geodesicPoint);
  }

  return points;
}

function markVector3(points: LonLat[]): [number, number] | undefined {
  for (let j = 1; j < points.length; j++) {
    const i = j - 1;
    const lon1 = points[i][0] + 180;
    const lon2 = points[j][0] + 180;
    if (
      Math.sin((lon1 * Math.PI) / 180) * Math.sin((lon2 * Math.PI) / 180) <
      0
    ) {
      return [i, j];
    }
  }

  return undefined;
}

export function toGeodesicPaths(
  from: LatLon,
  to: LatLon,
  numPoints: number
): Path[] {
  const xyzFrom = baseVector
    .clone()
    .applyQuaternion(latLonToQuat(from[0], from[1]));

  const xyzTo = baseVector.clone().applyQuaternion(latLonToQuat(to[0], to[1]));

  const vpoints = getGeodesicPoints(xyzFrom.clone(), xyzTo.clone(), numPoints);
  const lonLats = vpoints.map(xyzToLatLon).map((x) => [x[1], x[0]]);
  const markIndices = markVector3(lonLats);
  const paths: Path[] = [];

  if (markIndices !== undefined) {
    const lonLats1 = lonLats.slice(0, markIndices[0] + 1);
    if (lonLats1.length > 1) {
      paths.push({
        points: lonLats1,
      });
    }

    const lonLats2 = lonLats.slice(markIndices[0] + 1);
    if (lonLats2.length > 1) {
      paths.push({
        points: lonLats2,
      });
    }
    return paths;
  }
  return [
    {
      points: lonLats,
    },
  ];
}

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
  tooltip?: ReactNode;
  open?: boolean;
  index?: string;
  metadata?: Record<string, any>;
};

function setViewBox(svg: SVGSVGElement, viewBox: number[]): void {
  svg?.setAttribute("viewBox", viewBox.join(" "));
}

function getViewBox(svg: SVGSVGElement): number[] {
  const viewBox = svg?.getAttribute("viewBox");
  if (viewBox) {
    return viewBox.split(" ").map(Number);
  }
  return [0, 0, 0, 0];
}

export function useCanvasSizing(
  canvasW: number,
  canvasH: number,
  expanded: boolean,
  enableZoom: boolean
) {
  const canvasSvgRef = useRef<SVGSVGElement>(null);
  useEffect(() => {
    if (canvasSvgRef.current) {
      const svg = canvasSvgRef.current;
      const parent = svg.parentElement;
      if (parent) {
        const parentRect = parent.getBoundingClientRect();
        console.log("[dbg] canvas parentRect", parentRect);
        const realRatio = Math.max(0.3, parentRect.height / parentRect.width);
        const croppedCanvasY = canvasW * realRatio;
        const offsetX = 0;
        const offsetY = Math.max(0, (0.7 * (canvasH - croppedCanvasY)) / 2);
        const projXLen = canvasW;
        const projYLen = croppedCanvasY;

        setViewBox(svg, [offsetX, offsetY, projXLen, projYLen]);
      }
    }
  }, [canvasW, canvasH, canvasSvgRef.current, expanded]);

  useEffect(() => {
    const svg = canvasSvgRef.current;

    const onMouseDown = (event: MouseEvent) => {
      const x0 = event.clientX;
      const y0 = event.clientY;
      const [initOffsetX, initOffsetY, initProjXLen, initProjYLen] = getViewBox(
        svg!
      );
      const boundingBox = svg!.getBoundingClientRect();
      const onMouseMove = (event: MouseEvent) => {
        const x1 = event.clientX;
        const y1 = event.clientY;
        const dx = x1 - x0;
        const dy = y1 - y0;
        setViewBox(svg!, [
          initOffsetX - dx * (initProjXLen / boundingBox.width),
          initOffsetY - dy * (initProjYLen / boundingBox.height),
          initProjXLen,
          initProjYLen,
        ]);
        console.log(
          "[dbg] mouse move",
          dx,
          dy,
          initOffsetX - dx,
          initOffsetY - dy
        );
      };
      window.addEventListener("mousemove", onMouseMove);
      console.log("[dbg] added mouse move listener");
      const onMouseUp = (event: MouseEvent) => {
        window.removeEventListener("mousemove", onMouseMove);
        console.log("[dbg] removed mouse move listener");
        window.removeEventListener("mouseup", onMouseUp);
        console.log("[dbg] removed mouse up listener");
      };
      window.addEventListener("mouseup", onMouseUp);
    };
    svg?.addEventListener("mousedown", onMouseDown);
    console.log("[dbg] added mouse down listener");

    const onWheel = (event: WheelEvent) => {
      console.log("[dbg] wheel", event);
      const delta = event.deltaY;
      const [offsetX, offsetY, projXLen, projYLen] = getViewBox(svg!);
      const ratio = projYLen / projXLen;
      const newProjYLen = projYLen * (1 + delta / 280);
      const newProjXLen = newProjYLen / ratio;

      const deltaX = projXLen - newProjXLen;
      const deltaY = projYLen - newProjYLen;
      const newOffsetX = offsetX + deltaX / 2;
      const newOffsetY = offsetY + deltaY / 2;
      if (enableZoom) {
        setViewBox(svg!, [newOffsetX, newOffsetY, newProjXLen, newProjYLen]);
      }
    };
    window.addEventListener("wheel", onWheel);
    console.log("[dbg] added wheel listener");

    return () => {
      svg?.removeEventListener("mousedown", onMouseDown);
      console.log("[dbg] removed mouse down listener");
      window.removeEventListener("wheel", onWheel);
      console.log("[dbg] removed wheel listener");
    };
  }, [canvasSvgRef.current, expanded]);
  return { canvasSvgRef };
}

function RenderMarker(props: { marker: Marker; projector: Projector }) {
  const { marker, projector } = props;
  const [x, y] = projector(marker.lonLat);
  const [open, setOpen] = useState(!!marker.open);

  const radius = Math.max(1, marker.radius ?? 0);

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
        onClose={() => {
          if (!!marker.open) {
            return;
          }
          setOpen(false);
        }}
      >
        <g>
          <circle
            style={{ cursor: "default" }}
            strokeWidth={marker.strokeWidth}
            stroke={marker.stroke}
            cx={x}
            cy={y}
            r={radius}
            fill={marker.fill}
          />
          {marker.index && (
            <text
              x={x + radius * 1.75}
              y={y + radius * 1.75}
              fontSize={radius * 2}
              fill="white"
              style={{ cursor: "text" }}
            >
              {marker.index}
            </text>
          )}
        </g>
      </Tooltip>
    </Fragment>
  );
}

export type Path = {
  stroke?: CSSProperties["stroke"];
  strokeWidth?: CSSProperties["strokeWidth"];
  points: LonLat[];
};

function RenderPath(props: { path: Path; projector: Projector }) {
  const { path, projector } = props;
  if (path.points.length < 2) {
    return <Fragment></Fragment>;
  }
  const initPoint = path.points[0];
  const restPoints = path.points.slice(1);

  let d = `M ${projector(initPoint).join(" ")}`;
  for (const point of restPoints) {
    d += ` L ${projector(point).join(" ")}`;
  }

  return (
    <path
      d={d}
      stroke={path.stroke}
      strokeWidth={path.strokeWidth}
      fill="none"
    />
  );
}

export function WorldMap(props: {
  canvasWidth: number;
  canvasHeight: number;
  fill: CSSProperties["fill"];
  markers: Marker[];
  viewBox?: string;
  canvasSvgRef?: RefObject<SVGSVGElement>;
  paths?: Path[];
}) {
  const {
    canvasWidth: canvasX,
    canvasHeight: canvasY,
    fill,
    markers,
    viewBox,
    canvasSvgRef,
    paths,
  } = props;
  const flatShapes = useMemo(() => {
    const worldMap = worldMapAny as FeatureCollection;
    const flatShapes = toFlatShape(worldMap.features);
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
          style={{ overflow: "hidden", cursor: "grab" }}
        >
          <RenderPolygons
            shapes={flatShapes}
            projector={projector}
            fill={fill}
          />
          {paths?.map((path, idx) => (
            <RenderPath key={idx} path={path} projector={projector} />
          ))}
          {markers.map((marker, i) => (
            <RenderMarker key={i} marker={marker} projector={projector} />
          ))}
        </svg>
      </Box>
    </Fragment>
  );
}
