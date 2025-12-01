"use client";

import { Autocomplete, TextField } from "@mui/material";
import { useState, Fragment } from "react";
import { CircularProgress } from "@mui/material";

export function SourcesSelector(props: {
  value: string[];
  onChange: (value: string[]) => void;
  getOptions: () => Promise<string[]>;
}) {
  const { value, onChange, getOptions } = props;
  const [options, setOptions] = useState<string[]>([]);
  const [isLoading, setIsLoading] = useState<boolean>(false);
  const [isOpen, setIsOpen] = useState<boolean>(false);

  return (
    <Autocomplete
      fullWidth
      value={value}
      open={isOpen}
      onClose={() => setIsOpen(false)}
      onOpen={() => {
        setIsOpen(true);
        setIsLoading(true);
        getOptions()
          .then((options) =>
            setOptions(
              options.map((opt) => opt.trim()).filter((opt) => opt.length > 0)
            )
          )
          .finally(() => setIsLoading(false));
      }}
      onChange={(_, value) => onChange(value)}
      multiple
      options={options}
      defaultValue={[]}
      loading={isLoading}
      loadingText={"Loading..."}
      renderInput={(params) => (
        <TextField
          {...params}
          variant="standard"
          label="Sources"
          slotProps={{
            input: {
              ...params.InputProps,
              endAdornment: (
                <Fragment>
                  {isLoading ? (
                    <CircularProgress color="inherit" size={20} />
                  ) : null}
                  {params.InputProps.endAdornment}
                </Fragment>
              ),
            },
          }}
        />
      )}
      disableCloseOnSelect
    />
  );
}
