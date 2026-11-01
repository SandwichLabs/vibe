-- Assay for Neovim: `assay lsp` completion and hover in .sql files under an Assay
-- workspace (a folder with an assay.yaml), plus :AssayRun to run the buffer or a
-- selection with `assay query` and :AssaySnapshot to refresh the catalog.
--
--   require("assay").setup({ cmd = { "assay" } })
local M = {}

local defaults = {
  -- The assay executable (and any leading arguments).
  cmd = { "assay" },
  -- Filetypes the language server attaches to.
  filetypes = { "sql" },
  -- Extra arguments for :AssayRun, e.g. { "--format", "csv" }.
  run_args = {},
}

M.config = vim.deepcopy(defaults)

-- The workspace root for a buffer: $ASSAY_WORKSPACE (set when the TUI or
-- `assay query --edit` opened this editor), else the nearest assay.yaml above it.
function M.root(bufnr)
  local env = vim.env.ASSAY_WORKSPACE
  if env and env ~= "" then
    return env
  end
  return vim.fs.root(bufnr or 0, "assay.yaml")
end

local function assay(args)
  local cmd = vim.list_extend(vim.deepcopy(M.config.cmd), args)
  return cmd
end

local function enable_lsp()
  local spec = {
    cmd = assay({ "lsp" }),
    filetypes = M.config.filetypes,
    -- Start only inside a workspace: no assay.yaml, no server.
    root_dir = function(bufnr, on_dir)
      local root = M.root(bufnr)
      if root then
        on_dir(root)
      end
    end,
  }
  if vim.lsp.config and vim.lsp.enable then -- Neovim 0.11+
    vim.lsp.config("assay", spec)
    vim.lsp.enable("assay")
    return
  end
  -- Neovim 0.10: start it by hand per buffer.
  vim.api.nvim_create_autocmd("FileType", {
    group = vim.api.nvim_create_augroup("assay_lsp", { clear = true }),
    pattern = M.config.filetypes,
    callback = function(ev)
      local root = M.root(ev.buf)
      if root then
        vim.lsp.start({ name = "assay", cmd = spec.cmd, root_dir = root }, { bufnr = ev.buf })
      end
    end,
  })
end

-- Show lines in a reused scratch split.
local function show(lines)
  local name = "assay://results"
  local buf = vim.fn.bufnr(name)
  if buf == -1 then
    buf = vim.api.nvim_create_buf(false, true)
    vim.api.nvim_buf_set_name(buf, name)
    vim.bo[buf].bufhidden = "hide"
  end
  vim.bo[buf].modifiable = true
  vim.api.nvim_buf_set_lines(buf, 0, -1, false, lines)
  vim.bo[buf].modifiable = false
  if vim.fn.bufwinnr(buf) == -1 then
    vim.cmd("botright split")
    vim.api.nvim_win_set_buf(0, buf)
    vim.cmd("wincmd p")
  end
end

local function split_lines(s)
  return vim.split((s or ""):gsub("\n$", ""), "\n", { plain = true })
end

-- :AssayRun runs the buffer, or the lines of a range, through `assay query -`.
function M.run(opts)
  local bufnr = vim.api.nvim_get_current_buf()
  local root = M.root(bufnr)
  if not root then
    vim.notify("assay: not inside a workspace (no assay.yaml above this file)", vim.log.levels.WARN)
    return
  end
  local first, last = 0, -1
  if opts and opts.range and opts.range > 0 then
    first, last = opts.line1 - 1, opts.line2
  end
  local sql = table.concat(vim.api.nvim_buf_get_lines(bufnr, first, last, false), "\n")
  local args = vim.list_extend({ "query", "--workspace", root }, M.config.run_args)
  table.insert(args, "-")
  vim.system(assay(args), { stdin = sql, text = true }, function(res)
    vim.schedule(function()
      local lines = split_lines(res.stdout)
      if res.stderr and res.stderr ~= "" then
        vim.list_extend(lines, split_lines(res.stderr))
      end
      show(lines)
    end)
  end)
end

-- :AssaySnapshot rewrites the catalog snapshot the language server reads.
function M.snapshot()
  local root = M.root(0)
  if not root then
    vim.notify("assay: not inside a workspace", vim.log.levels.WARN)
    return
  end
  vim.system(assay({ "lsp", "snapshot", "--workspace", root }), { text = true }, function(res)
    vim.schedule(function()
      local level = res.code == 0 and vim.log.levels.INFO or vim.log.levels.WARN
      vim.notify("assay: " .. vim.trim((res.stdout or "") .. (res.stderr or "")), level)
    end)
  end)
end

function M.setup(opts)
  M.config = vim.tbl_deep_extend("force", vim.deepcopy(defaults), opts or {})
  enable_lsp()
  vim.api.nvim_create_user_command("AssayRun", M.run, { range = true, desc = "Run SQL with assay query" })
  vim.api.nvim_create_user_command("AssaySnapshot", M.snapshot, { desc = "Refresh the Assay catalog snapshot" })
end

return M
