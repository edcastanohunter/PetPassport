pdc_design = BA_PDC_PRODUCT_SEARCH
    concat('PDC:', cast(row.BOM AS String)) AS key,
    cast(row.BOM AS String) AS bom,
    substring(trim(cast(row.BOM AS String)), 1, 5) AS bomBase,
    cast(null AS String) AS bomFamily,
    cast(row.X_TECHNOLOGY AS String) AS technology,
    cast(row.X_CUTTER_SUMMARY AS String) AS cutterSummary,
    cast(null AS Long) AS cutterCount,
    try_cast(row.X_CUTTER_DIAMETER AS Double) AS cutterSize,


rc_design = BA_RC_PRODUCT_SEARCH
    concat('RC:', cast(row.ITEM AS String)) AS key,
    cast(row.ITEM AS String) AS bom,
    cast(null AS String) AS bomBase,
    cast(null AS String) AS bomFamily,
    cast(null AS String) AS technology,
    cast(null AS String) AS cutterSummary,
    cast(null AS Long) AS cutterCount,
    cast(null AS Double) AS cutterSize,


rhino_design = BA_RHINO_BLOCK
    concat('RHINO:', cast(row.BOM AS String)) AS key,
    cast(row.BOM AS String) AS bom,
    cast(null AS String) AS bomBase,
    cast(null AS String) AS bomFamily,
    cast(null AS String) AS cutterSummary,
    try_cast(row.`Total Cutter Count` AS Long) AS cutterCount,
    cast(null AS Double) AS cutterSize

xcd_design = BA_XCD
    concat('XCD:', cast(row.PN AS String)) AS key,
    cast(row.PN AS String) AS bom,
    cast(null AS String) AS bomBase,
    cast(row.`Brand Family` AS String) AS bomFamily,
    cast(null AS String) AS technology,
    cast(null AS String) AS cutterSummary,
    cast(null AS Long) AS cutterCount,
    try_cast(row.`Cutter Diameter (mm)` AS Double) AS cutterSize


    bits-raw-data-copier-edtest


?[r5sD!DBjfzO@YJ