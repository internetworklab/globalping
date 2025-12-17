import { Box, Tooltip } from "@mui/material";
import { useState } from "react";

export function IPDisp(props: { ip: string; rdns?: string }) {
  const { ip, rdns } = props;
  const [open, setOpen] = useState(false);
  return (
    <Tooltip
      onOpen={() => {
        if (rdns) {
          setOpen(true);
        }
      }}
      onClose={() => setOpen(false)}
      title={ip || rdns || ""}
      open={open}
    >
      <Box
        sx={{
          "&:hover": {
            outline: "1px solid red",
          },
        }}
      >
        {rdns ?? ip}
      </Box>
    </Tooltip>
  );
}
