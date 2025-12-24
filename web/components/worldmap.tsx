"use client";

import { Fragment, useEffect } from "react";
import worldMapAny from "./worldmap.json";
import { Box } from "@mui/material";

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

export function WorldMap() {
  useEffect(() => {
    const worldMap = worldMapAny as FeatureCollection;
    const flatShapes = toFlatShape(worldMap.features);
    console.log("[dbg] flatShapes", flatShapes);
  }, []);
  return (
    <Fragment>
      <Box sx={{ height: "100%" }}>Test Content</Box>
    </Fragment>
  );
}
