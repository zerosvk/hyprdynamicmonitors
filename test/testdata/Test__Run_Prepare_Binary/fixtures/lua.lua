-- <<<<< TUI AUTO START
hl.monitor({ output = "desc:BOE NE135A1M-NY1", mode = "2880x1920@120.00000", position = "0x0", scale = 2, transform = 0, vrr = 1 })
hl.monitor({ output = "DP-11", disabled = true })
hl.monitor({ output = "DP-12", disabled=true })
hl.monitor { output = "DP-13", disabled = true }
-- <<<<< TUI AUTO END

hl.monitor({
    output   = "desc:Dell Inc. DELL U2723QE",
    disabled = true, -- disabled = true in a comment stays untouched
})

-- hl.monitor({ output = "DP-14", disabled = true })
hl.monitor({ output = "disabled = true", mode = "preferred" })
hl.monitor({ output = "DP-15", disabled = false })

hl.config({
    decoration = {
        blur = { enabled = false },
    },
    misc = { disabled = true },
})
