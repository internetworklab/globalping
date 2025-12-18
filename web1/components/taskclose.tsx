"use client";

import {
  Typography,
  Button,
  Dialog,
  DialogTitle,
  DialogContent,
  DialogActions,
  IconButton,
  Tooltip,
} from "@mui/material";
import { Fragment, useState } from "react";
import CloseIcon from "@mui/icons-material/CloseOutlined";

export function TaskCloseIconButton(props: {
  onConfirmedClosed: () => void;
  taskId: string;
}) {
  const { onConfirmedClosed, taskId } = props;
  const [showDialog, setShowDialog] = useState<boolean>(false);
  return (
    <Fragment>
      <Tooltip title="Close Task">
        <IconButton
          onClick={() => {
            setShowDialog(true);
          }}
        >
          <CloseIcon />
        </IconButton>
      </Tooltip>
      <Dialog open={showDialog} onClose={() => setShowDialog(false)}>
        <DialogTitle>Close Task</DialogTitle>
        <DialogContent>
          <Typography>
            Are you sure you want to close task #{taskId}?
          </Typography>
        </DialogContent>
        <DialogActions>
          <Button onClick={() => setShowDialog(false)}>Cancel</Button>
          <Button onClick={onConfirmedClosed}>Close</Button>
        </DialogActions>
      </Dialog>
    </Fragment>
  );
}
