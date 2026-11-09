-- Some existing config
hl.monitor({ output = "", mode = "preferred", position = "auto", scale = 1 })
-- <<<<< TUI AUTO START
hl.monitor({ output = "desc:New Monitor A", mode = "2560x1440@120.00000", position = "0x0", scale = 1.5, transform = 0, vrr = 0 })
hl.monitor({ output = "desc:New Monitor B", mode = "1920x1080@60.00000", position = "2560x0", scale = 1, transform = 0, vrr = 1, bitdepth = 10, cm = "hdr", sdrbrightness = 1.10, sdrsaturation = 0.98, mirror = "eDP-1" })
hl.monitor({ output = "monC", mode = "1000x1000@60.00000", position = "-1000x-1000", scale = 1, transform = 0, vrr = 0 })
hl.monitor({ output = "desc:Dell #Whatever", mode = "1337x500@60.00000", position = "2137x-5000", scale = 2, transform = 2, vrr = 0 })
-- <<<<< TUI AUTO END
