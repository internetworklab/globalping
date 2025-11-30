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
} from "@mui/material";

type PingSample = {
  from: string;
  target: string;

  // in the unit of milliseconds
  latencyMs: number;
}

export default function Home() {
  const sources = [
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

  const targets = [
    "google.com",
    "github.com",
    "8.8.8.8",
    "x.com",
    "example.com",
    "1.1.1.1",
    "223.5.5.5",
  ];

  const pingSamples: PingSample[] = [];

  return (
    <Box sx={{ padding: 2 }}>
      <Card>
        <CardContent>
          <Typography variant="h6">Global Ping</Typography>
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
                  {sources.map((source) => (
                    <TableCell key={source}>{source}</TableCell>
                  ))}
                </TableRow>
              ))}
            </TableBody>
          </Table>
        </CardContent>
      </Card>
    </Box>
  );
}
