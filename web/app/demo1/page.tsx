"use client";

import { Box } from "@mui/material";
import { useEffect, useState } from "react";

function Line(props: { idx: number; text: string }) {
  return (
    <Box>
      [{props.idx}] {props.text}
    </Box>
  );
}

type RowObj = {
  id: number;
  text: string;
};

export default function Demo1Page() {
  const [rows, setRows] = useState<RowObj[]>([]);
  useEffect(() => {
    const it = window.setInterval(() => {
      setRows((prev) => {
        const newId = prev.length;
        const newContent = new Date().toISOString();
        const newObj: RowObj = {
          id: newId,
          text: newContent,
        };

        return [newObj, ...prev];
      });
    }, 1000);
    return () => {
      window.clearInterval(it);
    };
  }, []);

  return (
    <Box>
      <Box
        sx={{
          maxHeight: "100px",
          overflow: "auto",
          display: "flex",
          flexDirection: "column-reverse",
        }}
      >
        {rows.map((row) => (
          <Line key={row.id} idx={row.id} text={row.text} />
        ))}
      </Box>
    </Box>
  );
}
